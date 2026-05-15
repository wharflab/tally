package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDeprecatedBundlerInstallFlagsRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewDeprecatedBundlerInstallFlagsRule().Metadata()
	if meta.Code != DeprecatedBundlerInstallFlagsRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, DeprecatedBundlerInstallFlagsRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want %q", meta.Category, "correctness")
	}
}

func TestDeprecatedBundlerInstallFlagsRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewDeprecatedBundlerInstallFlagsRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "bundle install --without triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install --without development
`,
			WantViolations: 1,
		},
		{
			Name: "bundle install --without=test triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install --without=test
`,
			WantViolations: 1,
		},
		{
			Name: "bundle install --deployment triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install --deployment
`,
			WantViolations: 1,
		},
		{
			Name: "bundle install --path vendor/bundle triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install --path vendor/bundle
`,
			WantViolations: 1,
		},
		// --- Multiple flags: each gets a violation ---
		{
			Name: "bundle install --without --deployment triggers two violations",
			Content: `FROM ruby:3.3-slim
RUN bundle install --without development --deployment
`,
			WantViolations: 2,
		},
		// --- Compliance: no deprecated flags ---
		{
			Name: "bundle install with no deprecated flags suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle install --jobs 4
`,
			WantViolations: 0,
		},
		// --- Suppressions ---
		{
			Name: "dev stage skipped",
			Content: `FROM ruby:3.3-slim AS dev
RUN bundle install --without test
`,
			WantViolations: 0,
		},
	})
}

func TestDeprecatedBundlerInstallFlagsRule_FixSafetyVaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    string
		wantSafety rules.FixSafety
		wantEnvVar string
		wantInDesc string
	}{
		{
			name: "--without is FixSafe",
			content: `FROM ruby:3.3-slim
RUN bundle install --without development
`,
			wantSafety: rules.FixSafe,
			wantEnvVar: "BUNDLE_WITHOUT",
			wantInDesc: "BUNDLE_WITHOUT",
		},
		{
			name: "--deployment is FixSafe",
			content: `FROM ruby:3.3-slim
RUN bundle install --deployment
`,
			wantSafety: rules.FixSafe,
			wantEnvVar: "BUNDLE_DEPLOYMENT",
			wantInDesc: "BUNDLE_DEPLOYMENT",
		},
		{
			name: "--path is FixSuggestion (downstream BUNDLE_PATH may differ)",
			content: `FROM ruby:3.3-slim
RUN bundle install --path vendor/bundle
`,
			wantSafety: rules.FixSuggestion,
			wantEnvVar: "BUNDLE_PATH",
			wantInDesc: "BUNDLE_PATH",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			violations := NewDeprecatedBundlerInstallFlagsRule().Check(input)
			if len(violations) != 1 {
				t.Fatalf("got %d violations, want 1", len(violations))
			}
			v := violations[0]
			if v.SuggestedFix == nil {
				t.Fatal("expected a suggested fix")
			}
			if v.SuggestedFix.Safety != tt.wantSafety {
				t.Errorf("Safety = %v, want %v", v.SuggestedFix.Safety, tt.wantSafety)
			}
			if !strings.Contains(v.SuggestedFix.Description, tt.wantInDesc) {
				t.Errorf("description should mention %q; got: %q",
					tt.wantInDesc, v.SuggestedFix.Description)
			}
		})
	}
}
