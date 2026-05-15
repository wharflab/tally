package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestHealthcheckRailsUpEndpointRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewHealthcheckRailsUpEndpointRule().Metadata()
	if meta.Code != HealthcheckRailsUpEndpointRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, HealthcheckRailsUpEndpointRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want %q", meta.Category, "correctness")
	}
}

func TestHealthcheckRailsUpEndpointRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewHealthcheckRailsUpEndpointRule(), []testutil.RuleTestCase{
		// --- Variant 1: missing HEALTHCHECK ---
		{
			Name: "Rails runtime without HEALTHCHECK triggers",
			Content: `FROM ruby:3.3-slim
CMD ["bin/rails", "server"]
`,
			WantViolations: 1,
		},
		{
			Name: "puma runtime without HEALTHCHECK triggers",
			Content: `FROM ruby:3.3-slim
CMD ["puma"]
`,
			WantViolations: 1,
		},
		// --- Variant 2: HEALTHCHECK NONE suppresses ---
		{
			Name: "HEALTHCHECK NONE explicitly opts out",
			Content: `FROM ruby:3.3-slim
HEALTHCHECK NONE
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		// --- Variant 3: curl-based HEALTHCHECK ---
		{
			Name: "HEALTHCHECK with curl triggers",
			Content: `FROM ruby:3.3-slim
HEALTHCHECK CMD curl -fsS http://127.0.0.1:3000/up || exit 1
CMD ["bin/rails", "server"]
`,
			WantViolations: 1,
		},
		{
			Name: "HEALTHCHECK with wget triggers",
			Content: `FROM ruby:3.3-slim
HEALTHCHECK CMD wget -q --spider http://127.0.0.1:3000/up
CMD ["bin/rails", "server"]
`,
			WantViolations: 1,
		},
		// --- Compliance: Ruby-native HEALTHCHECK ---
		{
			Name: "HEALTHCHECK with ruby -rnet/http suppresses",
			Content: `FROM ruby:3.3-slim
HEALTHCHECK CMD ["ruby", "-rnet/http", "-e", "exit 0"]
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		// --- Suppressions ---
		{
			Name: "non-Ruby image skipped",
			Content: `FROM debian:bookworm-slim
CMD ["nginx"]
`,
			WantViolations: 0,
		},
		{
			Name: "Ruby CLI image (no rails/puma/etc.) skipped",
			Content: `FROM ruby:3.3-slim
ENTRYPOINT ["mygem"]
`,
			WantViolations: 0,
		},
		{
			Name: "sidekiq worker stage skipped (not a web server)",
			Content: `FROM ruby:3.3-slim
CMD ["bundle", "exec", "sidekiq"]
`,
			WantViolations: 0,
		},
		{
			Name: "dev stage skipped",
			Content: `FROM ruby:3.3-slim AS dev
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
	})
}

func TestHealthcheckRailsUpEndpointRule_FixDescriptionMentionsCanonicalForm(t *testing.T) {
	t.Parallel()

	content := `FROM ruby:3.3-slim
CMD ["bin/rails", "server"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewHealthcheckRailsUpEndpointRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a suggested fix")
	}
	if !strings.Contains(v.SuggestedFix.Description, "Net::HTTP") {
		t.Errorf("description should mention Net::HTTP; got: %q", v.SuggestedFix.Description)
	}
	if !strings.Contains(v.SuggestedFix.Description, "/up") {
		t.Errorf("description should mention /up; got: %q", v.SuggestedFix.Description)
	}
}
