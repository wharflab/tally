package lspserver

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/rules"
)

func TestHasFixAllCandidate(t *testing.T) {
	t.Parallel()

	filePath := filepath.Clean("/tmp/Dockerfile")
	safeEdit := rules.TextEdit{
		Location: rules.NewLineLocation(filePath, 1),
		NewText:  "FROM alpine:3.19",
	}

	tests := []struct {
		name       string
		cfg        *config.Config
		violations []rules.Violation
		want       bool
	}{
		{
			name: "safe fix with edits",
			violations: []rules.Violation{
				violationWithFix(filePath, "buildkit/MaintainerDeprecated", &rules.SuggestedFix{
					Description: "replace maintainer",
					Edits:       []rules.TextEdit{safeEdit},
					Safety:      rules.FixSafe,
				}),
			},
			want: true,
		},
		{
			name: "safe async fix",
			violations: []rules.Violation{
				violationWithFix(filePath, "tally/prefer-run-heredoc", &rules.SuggestedFix{
					Description:  "convert to heredoc",
					Safety:       rules.FixSafe,
					NeedsResolve: true,
					ResolverID:   "test",
				}),
			},
			want: true,
		},
		{
			name: "no suggested fix",
			violations: []rules.Violation{
				rules.NewViolation(rules.NewLineLocation(filePath, 1), "tally/no-multi-spaces", "msg", rules.SeverityWarning),
			},
			want: false,
		},
		{
			name: "unsafe fix only",
			violations: []rules.Violation{
				violationWithFix(filePath, "tally/prefer-multi-stage-build", &rules.SuggestedFix{
					Description: "split stages",
					Edits:       []rules.TextEdit{safeEdit},
					Safety:      rules.FixUnsafe,
				}),
			},
			want: false,
		},
		{
			name: "safe fix without edits or resolver",
			violations: []rules.Violation{
				violationWithFix(filePath, "buildkit/MaintainerDeprecated", &rules.SuggestedFix{
					Description: "broken fix",
					Safety:      rules.FixSafe,
				}),
			},
			want: false,
		},
		{
			name: "safe fix gated by unsafe-only mode",
			cfg: &config.Config{
				Rules: config.RulesConfig{
					Buildkit: map[string]config.RuleConfig{
						"MaintainerDeprecated": {Fix: config.FixModeUnsafeOnly},
					},
				},
			},
			violations: []rules.Violation{
				violationWithFix(filePath, "buildkit/MaintainerDeprecated", &rules.SuggestedFix{
					Description: "replace maintainer",
					Edits:       []rules.TextEdit{safeEdit},
					Safety:      rules.FixSafe,
				}),
			},
			want: false,
		},
		{
			name: "safe fix gated by never mode",
			cfg: &config.Config{
				Rules: config.RulesConfig{
					Buildkit: map[string]config.RuleConfig{
						"MaintainerDeprecated": {Fix: config.FixModeNever},
					},
				},
			},
			violations: []rules.Violation{
				violationWithFix(filePath, "buildkit/MaintainerDeprecated", &rules.SuggestedFix{
					Description: "replace maintainer",
					Edits:       []rules.TextEdit{safeEdit},
					Safety:      rules.FixSafe,
				}),
			},
			want: false,
		},
		{
			name: "safe fix gated by explicit mode",
			cfg: &config.Config{
				Rules: config.RulesConfig{
					Buildkit: map[string]config.RuleConfig{
						"MaintainerDeprecated": {Fix: config.FixModeExplicit},
					},
				},
			},
			violations: []rules.Violation{
				violationWithFix(filePath, "buildkit/MaintainerDeprecated", &rules.SuggestedFix{
					Description: "replace maintainer",
					Edits:       []rules.TextEdit{safeEdit},
					Safety:      rules.FixSafe,
				}),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, hasFixAllCandidate(tt.violations, tt.cfg))
		})
	}
}

func TestHasFixAllCandidate_MultiFix(t *testing.T) {
	t.Parallel()

	filePath := filepath.Clean("/tmp/Dockerfile")
	safeEdit := rules.TextEdit{
		Location: rules.NewLineLocation(filePath, 1),
		NewText:  "# commented: STOPSIGNAL SIGKILL",
	}

	tests := []struct {
		name       string
		violations []rules.Violation
		want       bool
	}{
		{
			name: "preferred fix is safe",
			violations: []rules.Violation{
				violationWithFixes(filePath, "tally/windows/no-stopsignal", []*rules.SuggestedFix{
					{Description: "Comment out", Safety: rules.FixSafe, IsPreferred: true, Edits: []rules.TextEdit{safeEdit}},
					{Description: "Delete line", Safety: rules.FixSuggestion, Edits: []rules.TextEdit{safeEdit}},
				}),
			},
			want: true,
		},
		{
			name: "preferred fix is unsafe",
			violations: []rules.Violation{
				violationWithFixes(filePath, "tally/prefer-multi-stage-build", []*rules.SuggestedFix{
					{Description: "Refactor", Safety: rules.FixUnsafe, IsPreferred: true, Edits: []rules.TextEdit{safeEdit}},
					{Description: "Comment out", Safety: rules.FixSafe, Edits: []rules.TextEdit{safeEdit}},
				}),
			},
			want: false, // fix-all uses preferred fix, which is unsafe
		},
		{
			name: "preferred fix is suggestion",
			violations: []rules.Violation{
				violationWithFixes(filePath, "hadolint/DL3001", []*rules.SuggestedFix{
					{Description: "Better fix", Safety: rules.FixSuggestion, IsPreferred: true, Edits: []rules.TextEdit{safeEdit}},
					{Description: "Safe fix", Safety: rules.FixSafe, Edits: []rules.TextEdit{safeEdit}},
				}),
			},
			want: false, // fix-all uses preferred fix, which is suggestion (not safe)
		},
		{
			name: "no preferred marked defaults to first which is safe",
			violations: []rules.Violation{
				violationWithFixes(filePath, "buildkit/MaintainerDeprecated", []*rules.SuggestedFix{
					{Description: "Safe fix", Safety: rules.FixSafe, Edits: []rules.TextEdit{safeEdit}},
					{Description: "Suggestion", Safety: rules.FixSuggestion, Edits: []rules.TextEdit{safeEdit}},
				}),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, hasFixAllCandidate(tt.violations, nil))
		})
	}
}

func violationWithFixes(filePath, ruleCode string, fixes []*rules.SuggestedFix) rules.Violation {
	return rules.NewViolation(
		rules.NewLineLocation(filePath, 1),
		ruleCode,
		"msg",
		rules.SeverityWarning,
	).WithSuggestedFixes(fixes)
}

func violationWithFix(filePath, ruleCode string, suggestedFix *rules.SuggestedFix) rules.Violation {
	return rules.NewViolation(
		rules.NewLineLocation(filePath, 1),
		ruleCode,
		"msg",
		rules.SeverityWarning,
	).WithSuggestedFix(suggestedFix)
}
