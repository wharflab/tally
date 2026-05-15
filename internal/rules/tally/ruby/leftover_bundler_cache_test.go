package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestLeftoverBundlerCacheRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewLeftoverBundlerCacheRule().Metadata()
	if meta.Code != LeftoverBundlerCacheRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, LeftoverBundlerCacheRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "performance" {
		t.Errorf("Category = %q, want %q", meta.Category, "performance")
	}
	if !strings.HasPrefix(meta.DocURL, "https://tally.wharflab.com/rules/tally/ruby/") {
		t.Errorf("DocURL = %q, want tally/ruby/ prefix", meta.DocURL)
	}
}

func TestLeftoverBundlerCacheRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewLeftoverBundlerCacheRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "bundle install without cleanup triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 1,
		},
		// --- Compliance: cleanup in same RUN ---
		{
			Name: "bundle install with Rails-generator cleanup in same RUN suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle install \
    && rm -rf ~/.bundle/ "${BUNDLE_PATH}"/ruby/*/cache "${BUNDLE_PATH}"/ruby/*/bundler/gems/*/.git
`,
			WantViolations: 0,
		},
		{
			Name: "cleanup of just ~/.bundle suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle install \
    && rm -rf ~/.bundle/
`,
			WantViolations: 0,
		},
		// --- Compliance: cleanup in later RUN ---
		{
			Name: "cleanup in a later RUN after install suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle install
RUN rm -rf ~/.bundle/
`,
			WantViolations: 0,
		},
		// --- Compliance: bundle clean --force ---
		{
			Name: "bundle clean --force after install suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle install \
    && bundle clean --force
`,
			WantViolations: 0,
		},
		// --- Suppressions ---
		{
			Name: "dev stage skipped",
			Content: `FROM ruby:3.3-slim AS dev
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "non-Ruby stage skipped",
			Content: `FROM debian:bookworm-slim
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "builder stage exporting via COPY --from skipped",
			Content: `FROM ruby:3.3-slim AS builder
RUN bundle install

FROM ruby:3.3-slim
COPY --from=builder /usr/local/bundle /usr/local/bundle
`,
			WantViolations: 0,
		},
		// --- Ordering: cleanup BEFORE install does NOT count ---
		{
			Name: "cleanup before install does NOT suppress",
			Content: `FROM ruby:3.3-slim
RUN rm -rf ~/.bundle/
RUN bundle install
`,
			WantViolations: 1,
		},
	})
}

func TestLeftoverBundlerCacheRule_FixSafety(t *testing.T) {
	t.Parallel()

	content := `FROM ruby:3.3-slim
RUN bundle install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewLeftoverBundlerCacheRule().Check(input)
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
	wantParts := []string{"~/.bundle/", "BUNDLE_PATH", "/cache", "/bundler/gems"}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Errorf("fix should contain %q; got: %q", want, got)
		}
	}
}
