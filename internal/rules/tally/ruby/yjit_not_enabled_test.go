package ruby

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestYJITNotEnabledOnSupportedRuntimeRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewYJITNotEnabledOnSupportedRuntimeRule().Metadata()
	if meta.Code != YJITNotEnabledOnSupportedRuntimeRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, YJITNotEnabledOnSupportedRuntimeRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "performance" {
		t.Errorf("Category = %q, want %q", meta.Category, "performance")
	}
}

func TestYJITNotEnabledOnSupportedRuntimeRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewYJITNotEnabledOnSupportedRuntimeRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "ruby:3.3-slim runtime without YJIT triggers",
			Content: `FROM ruby:3.3-slim
CMD ["bin/rails", "server"]
`,
			WantViolations: 1,
		},
		{
			Name: "ruby:3.4 runtime without YJIT triggers",
			Content: `FROM ruby:3.4
CMD ["puma"]
`,
			WantViolations: 1,
		},
		// --- Compliance: ENV RUBY_YJIT_ENABLE=1 ---
		{
			Name: "ENV RUBY_YJIT_ENABLE=1 suppresses",
			Content: `FROM ruby:3.3-slim
ENV RUBY_YJIT_ENABLE="1"
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		// --- Compliance: RUBYOPT contains --yjit ---
		{
			Name: "ENV RUBYOPT containing --yjit suppresses",
			Content: `FROM ruby:3.3-slim
ENV RUBYOPT="--yjit"
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		// --- Compliance: --yjit on the entrypoint ---
		{
			Name: "CMD passing --yjit to bin/rails suppresses",
			Content: `FROM ruby:3.3-slim
CMD ["bin/rails", "server", "--yjit"]
`,
			WantViolations: 0,
		},
		// --- Suppressions ---
		{
			Name: "ruby:3.2 (pre-3.3) does NOT trigger (YJIT was experimental)",
			Content: `FROM ruby:3.2-slim
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		{
			Name: "ruby:3.0 does NOT trigger",
			Content: `FROM ruby:3.0-slim
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
		{
			Name: "non-Ruby runtime does NOT trigger",
			Content: `FROM debian:bookworm-slim
CMD ["bash"]
`,
			WantViolations: 0,
		},
		{
			Name: "Ruby CLI image (no rails/puma/etc.) does NOT trigger",
			Content: `FROM ruby:3.3-slim
ENTRYPOINT ["mygem"]
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
		// --- Multi-stage: only final stage matters ---
		{
			Name: "non-final ruby:3.3 stage does NOT trigger (rule scopes to final)",
			Content: `FROM ruby:3.3-slim AS builder
CMD ["bin/rails", "server"]

FROM ruby:3.2-slim
CMD ["bin/rails", "server"]
`,
			WantViolations: 0,
		},
	})
}

func TestYJITNotEnabledOnSupportedRuntimeRule_FixSafety(t *testing.T) {
	t.Parallel()

	content := `FROM ruby:3.3-slim
CMD ["bin/rails", "server"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewYJITNotEnabledOnSupportedRuntimeRule().Check(input)
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
}
