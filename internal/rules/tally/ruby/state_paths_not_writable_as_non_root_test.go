package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestStatePathsNotWritableAsNonRootRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewStatePathsNotWritableAsNonRootRule().Metadata()
	if meta.Code != StatePathsNotWritableAsNonRootRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, StatePathsNotWritableAsNonRootRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want %q", meta.Category, "correctness")
	}
}

func TestStatePathsNotWritableAsNonRootRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewStatePathsNotWritableAsNonRootRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "non-root USER + COPY . . without --chown triggers",
			Content: `FROM ruby:3.3-slim
WORKDIR /rails
USER rails:rails
COPY . .
CMD ["bin/rails", "server"]
`,
			WantViolations: 1,
		},
		{
			Name: "USER set as numeric uid + COPY without --chown still triggers",
			Content: `FROM ruby:3.3-slim
WORKDIR /rails
USER 1000:1000
COPY . .
CMD ["bin/rails", "server"]
`,
			WantViolations: 1,
		},
		{
			Name: "COPY --from=builder /rails /rails without --chown triggers",
			Content: `FROM ruby:3.3-slim AS builder
WORKDIR /rails
COPY . .

FROM ruby:3.3-slim
WORKDIR /rails
USER rails:rails
COPY --from=builder /rails /rails
CMD ["bin/rails", "server"]
`,
			WantViolations: 1,
		},
		// --- Compliance: --chown on the COPY ---
		{
			Name: "COPY --chown=rails:rails . . suppresses",
			Content: `FROM ruby:3.3-slim
WORKDIR /rails
USER rails:rails
COPY --chown=rails:rails . .
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		{
			Name: "COPY --chown=rails . . (bare user, no group) suppresses",
			Content: `FROM ruby:3.3-slim
WORKDIR /rails
USER rails
COPY --chown=rails . .
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		// --- Compliance: chown -R covering all state dirs ---
		{
			Name: "chown -R rails:rails db log storage tmp before USER suppresses",
			Content: `FROM ruby:3.3-slim
WORKDIR /rails
COPY . .
RUN chown -R rails:rails db log storage tmp
USER rails:rails
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		// --- Suppressions ---
		{
			Name: "root USER skipped",
			Content: `FROM ruby:3.3-slim
WORKDIR /rails
USER root
COPY . .
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		{
			Name: "no USER instruction skipped",
			Content: `FROM ruby:3.3-slim
WORKDIR /rails
COPY . .
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		{
			Name: "non-Rails image (CLI gem) skipped",
			Content: `FROM ruby:3.3-slim
USER ruby:ruby
COPY mygem.gemspec /
CMD ["mygem"]
`,
			WantViolations: 0,
		},
		{
			Name: "non-Ruby base skipped",
			Content: `FROM debian:bookworm-slim
USER nonroot:nonroot
COPY . .
`,
			WantViolations: 0,
		},
	})
}

func TestStatePathsNotWritableAsNonRootRule_FixAddsChownFlag(t *testing.T) {
	t.Parallel()

	content := `FROM ruby:3.3-slim
WORKDIR /rails
USER rails:rails
COPY . .
CMD ["bin/rails", "server"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewStatePathsNotWritableAsNonRootRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a suggested fix")
	}
	if v.SuggestedFix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
	}
	got := v.SuggestedFix.Edits[0].NewText
	want := "--chown=rails:rails "
	if !strings.Contains(got, want) {
		t.Errorf("fix should contain %q; got: %q", want, got)
	}
}
