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

func violationWithFix(filePath, ruleCode string, suggestedFix *rules.SuggestedFix) rules.Violation {
	return rules.NewViolation(
		rules.NewLineLocation(filePath, 1),
		ruleCode,
		"msg",
		rules.SeverityWarning,
	).WithSuggestedFix(suggestedFix)
}
