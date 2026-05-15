package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferBundlerCacheMountRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewPreferBundlerCacheMountRule().Metadata()
	if meta.Code != PreferBundlerCacheMountRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, PreferBundlerCacheMountRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "performance" {
		t.Errorf("Category = %q, want %q", meta.Category, "performance")
	}
}

func TestPreferBundlerCacheMountRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferBundlerCacheMountRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "bundle install without cache mount triggers (with syntax pragma)",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 1,
		},
		// --- Compliance: cache mount on ${BUNDLE_PATH}/cache ---
		{
			Name: "bundle install with cache mount on BUNDLE_PATH suppresses",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
RUN --mount=type=cache,id=bundler,target=/usr/local/bundle/cache,sharing=locked \
    bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "bundle install with cache mount on /bundle/cache suppresses",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
RUN --mount=type=cache,target=/bundle/cache \
    bundle install
`,
			WantViolations: 0,
		},
		// --- Suppressions ---
		{
			Name: "no syntax pragma → suppress (cache mounts unsupported)",
			Content: `FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "dev stage skipped",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim AS dev
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "non-Ruby stage skipped",
			Content: `# syntax=docker/dockerfile:1
FROM debian:bookworm-slim
RUN bundle install
`,
			WantViolations: 0,
		},
	})
}

func TestPreferBundlerCacheMountRule_FixSafety(t *testing.T) {
	t.Parallel()

	content := `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
RUN bundle install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferBundlerCacheMountRule().Check(input)
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
	// No-edit suggestion: rewriting --mount onto multi-line RUNs is too
	// risky. The user applies the suggestion manually.
	if len(v.SuggestedFix.Edits) != 0 {
		t.Errorf("expected no edits (manual fix); got %d", len(v.SuggestedFix.Edits))
	}
	if !strings.Contains(v.SuggestedFix.Description, "cache") {
		t.Errorf("description should mention cache mount; got: %q",
			v.SuggestedFix.Description)
	}
}

func TestCacheTargetMatchesBundlerCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target string
		want   bool
	}{
		{"/usr/local/bundle/cache", true},
		{"/bundle/cache", true},
		{"/bundle-cache", true},
		{"/cache", true},
		{"${BUNDLE_PATH}/cache", true},
		{"$BUNDLE_PATH/cache", true},
		{"/tmp", false},
		{"/var/cache", false},
		{"", false},
	}
	for _, tt := range tests {
		got := cacheTargetMatchesBundlerCache(tt.target)
		if got != tt.want {
			t.Errorf("cacheTargetMatchesBundlerCache(%q) = %v, want %v", tt.target, got, tt.want)
		}
	}
}
