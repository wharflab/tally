package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestMissingBundleDeploymentRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewMissingBundleDeploymentRule().Metadata()
	if meta.Code != MissingBundleDeploymentRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, MissingBundleDeploymentRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want %q", meta.Category, "correctness")
	}
	if meta.FixPriority != missingBundleDeploymentFixPriority {
		t.Errorf("FixPriority = %d, want %d", meta.FixPriority, missingBundleDeploymentFixPriority)
	}
	if !strings.HasPrefix(meta.DocURL, "https://tally.wharflab.com/rules/tally/ruby/") {
		t.Errorf("DocURL = %q, want tally/ruby/ prefix", meta.DocURL)
	}
}

func TestMissingBundleDeploymentRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewMissingBundleDeploymentRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "ruby base running bundle install without BUNDLE_DEPLOYMENT triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "shell-form bundle install with extra flags triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install --jobs=4 --retry=3
`,
			WantViolations: 1,
		},
		{
			Name: "BUNDLE_DEPLOYMENT explicitly false still triggers",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_DEPLOYMENT="false"
RUN bundle install
`,
			WantViolations: 1,
		},
		// --- Regression: ordering matters ---
		{
			Name: "ENV BUNDLE_DEPLOYMENT after the install does NOT suppress (Docker ENV is forward-only)",
			Content: `FROM ruby:3.3-slim
RUN bundle install
ENV BUNDLE_DEPLOYMENT="1"
`,
			WantViolations: 1,
		},
		{
			Name: "bundle config set deployment in a later RUN does NOT suppress an earlier install",
			Content: `FROM ruby:3.3-slim
RUN bundle install
RUN bundle config set --local deployment 'true'
`,
			WantViolations: 1,
		},
		// --- Compliance: ENV BUNDLE_DEPLOYMENT=1 ---
		{
			Name: "ENV BUNDLE_DEPLOYMENT=1 (numeric) suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_DEPLOYMENT="1"
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "ENV BUNDLE_DEPLOYMENT=true suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_DEPLOYMENT=true
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- Compliance: bundle config set deployment ---
		{
			Name: "bundle config set deployment 'true' before install in same RUN suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle config set --local deployment 'true' && bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "bundle config set deployment in earlier RUN suppresses subsequent install",
			Content: `FROM ruby:3.3-slim
RUN bundle config set --global deployment true
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- Compliance: bundle install --deployment (deprecated, but compliant for THIS rule) ---
		{
			Name: "bundle install --deployment (deprecated 2.x flag) suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle install --deployment
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
			Name: "RAILS_ENV=development demotes",
			Content: `FROM ruby:3.3-slim
ENV RAILS_ENV="development"
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
		// --- Frozen-only is partial-compliance: rule still fires, detail mentions frozen ---
		{
			Name: "bundle config set frozen alone still triggers (frozen != deployment)",
			Content: `FROM ruby:3.3-slim
RUN bundle config set --local frozen 'true' && bundle install
`,
			WantViolations: 1,
		},
	})
}

func TestMissingBundleDeploymentRule_FixSafety(t *testing.T) {
	t.Parallel()

	content := `FROM ruby:3.3-slim
ENV RAILS_ENV="production"
RUN bundle install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewMissingBundleDeploymentRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a suggested fix")
	}
	if v.SuggestedFix.Safety != rules.FixSafe {
		t.Errorf("Safety = %v, want FixSafe", v.SuggestedFix.Safety)
	}
	if !strings.Contains(v.SuggestedFix.Edits[0].NewText, `BUNDLE_DEPLOYMENT="1"`) {
		t.Errorf("fix text missing BUNDLE_DEPLOYMENT: %q", v.SuggestedFix.Edits[0].NewText)
	}
}

// TestMissingBundleDeploymentRule_FixHandlesFROMLineContinuation regression-tests
// the case where `FROM` uses a backslash line continuation. The parser-reported
// stage location ends on the first physical line of the FROM, but the
// fix needs to insert AFTER the final continuation line — otherwise the
// new ENV line lands inside the FROM, producing a syntactically broken
// Dockerfile. The shared helper resolves the actual end via SourceMap's
// ResolveEndLine.
func TestMissingBundleDeploymentRule_FixHandlesFROMLineContinuation(t *testing.T) {
	t.Parallel()

	src := "FROM \\\n    ruby:3.3-slim\n" +
		"ENV RAILS_ENV=\"production\"\n" +
		"RUN bundle install\n"
	input := testutil.MakeLintInput(t, "Dockerfile", src)
	violations := NewMissingBundleDeploymentRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a suggested fix")
	}
	got := string(fix.ApplyFix([]byte(src), v.SuggestedFix))
	want := "FROM \\\n    ruby:3.3-slim\n" +
		"ENV BUNDLE_DEPLOYMENT=\"1\"\n" +
		"ENV RAILS_ENV=\"production\"\n" +
		"RUN bundle install\n"
	if got != want {
		t.Errorf("fix landed inside the FROM continuation;\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestMissingBundleDeploymentRule_FrozenSignalAddsDetailText(t *testing.T) {
	t.Parallel()

	// Frozen-only configuration: the rule still fires but the detail text
	// should mention that BUNDLE_DEPLOYMENT is the strict superset.
	content := `FROM ruby:3.3-slim
ENV RAILS_ENV="production"
RUN bundle config set --local frozen 'true' && bundle install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewMissingBundleDeploymentRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if !strings.Contains(violations[0].Detail, "strict superset") {
		t.Errorf("detail should mention 'strict superset' when frozen is set; got: %q",
			violations[0].Detail)
	}
}
