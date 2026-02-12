package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/heredoc"
	"github.com/tinovyatkin/tally/internal/shell"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestPreferHeredocRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferHeredocRule().Metadata())
}

func TestPreferHeredocRule_DefaultConfig(t *testing.T) {
	t.Parallel()
	rule := NewPreferHeredocRule()
	cfg, ok := rule.DefaultConfig().(PreferHeredocConfig)
	if !ok {
		t.Fatal("DefaultConfig did not return PreferHeredocConfig")
	}

	if cfg.MinCommands == nil || *cfg.MinCommands != 3 {
		t.Errorf("MinCommands = %v, want 3", cfg.MinCommands)
	}
	if cfg.CheckConsecutiveRuns == nil || !*cfg.CheckConsecutiveRuns {
		t.Errorf("CheckConsecutiveRuns = %v, want true", cfg.CheckConsecutiveRuns)
	}
	if cfg.CheckChainedCommands == nil || !*cfg.CheckChainedCommands {
		t.Errorf("CheckChainedCommands = %v, want true", cfg.CheckChainedCommands)
	}
}

func TestPreferHeredocRule_Check(t *testing.T) {
	t.Parallel()
	testutil.RunRuleTests(t, NewPreferHeredocRule(), []testutil.RuleTestCase{
		{
			Name: "three consecutive RUNs",
			Content: `FROM alpine
RUN echo 1
RUN echo 2
RUN echo 3
`,
			WantViolations: 1,
		},
		{
			Name: "two consecutive RUNs - no violation",
			Content: `FROM alpine
RUN echo 1
RUN echo 2
`,
			WantViolations: 0,
		},
		{
			Name: "three chained commands",
			Content: `FROM alpine
RUN apt-get update && apt-get upgrade -y && apt-get install -y vim
`,
			WantViolations: 1,
		},
		{
			Name: "two chained commands - no violation",
			Content: `FROM alpine
RUN apt-get update && apt-get install -y vim
`,
			WantViolations: 0,
		},
		{
			Name: "exec form breaks sequence",
			Content: `FROM alpine
RUN echo 1
RUN ["echo", "2"]
RUN echo 3
`,
			WantViolations: 0,
		},
		{
			Name: "non-RUN breaks sequence",
			Content: `FROM alpine
RUN echo 1
WORKDIR /app
RUN echo 2
`,
			WantViolations: 0,
		},
		{
			Name: "heredoc already used - no chained violation",
			Content: `FROM alpine
RUN <<EOF
echo 1
echo 2
EOF
`,
			WantViolations: 0,
		},
		{
			Name: "disable consecutive check",
			Content: `FROM alpine
RUN echo 1
RUN echo 2
RUN echo 3
`,
			Config: map[string]any{
				"check-consecutive-runs": false,
			},
			WantViolations: 0,
		},
		{
			Name: "disable chained check",
			Content: `FROM alpine
RUN apt-get update && apt-get upgrade -y && apt-get install -y vim
`,
			Config: map[string]any{
				"check-chained-commands": false,
			},
			WantViolations: 0,
		},
		{
			Name: "custom min-commands threshold",
			Content: `FROM alpine
RUN echo 1
RUN echo 2
RUN echo 3
RUN echo 4
`,
			Config: map[string]any{
				"min-commands": 5,
			},
			WantViolations: 0, // Only 4 commands, need 5
		},
		{
			Name: "custom min-commands met",
			Content: `FROM alpine
RUN echo 1
RUN echo 2
RUN echo 3
RUN echo 4
RUN echo 5
`,
			Config: map[string]any{
				"min-commands": 5,
			},
			WantViolations: 1,
		},
		{
			Name: "heredoc with exit breaks sequence",
			Content: `FROM alpine
RUN echo 1
RUN <<EOF
if [ ! -f /etc/foo ]; then
  exit 0
fi
echo setup
EOF
RUN echo 2
`,
			WantViolations: 0, // exit in heredoc breaks the sequence, only 1+1=2 commands on each side
		},
		{
			Name: "heredoc without exit allows sequence",
			Content: `FROM alpine
RUN echo 1
RUN <<EOF
echo setup
echo more
EOF
RUN echo 2
`,
			WantViolations: 1, // no exit, 4 commands total (1 + 2 + 1)
		},
	})
}

func TestPreferHeredocRule_CheckWithFixes(t *testing.T) {
	t.Parallel()
	rule := NewPreferHeredocRule()

	tests := []struct {
		name       string
		content    string
		wantHasFix bool
	}{
		{
			name: "simple consecutive has fix",
			content: `FROM alpine
RUN echo 1
RUN echo 2
RUN echo 3
`,
			wantHasFix: true,
		},
		{
			name: "simple chained has fix",
			content: `FROM alpine
RUN apt-get update && apt-get upgrade -y && apt-get install -y vim
`,
			wantHasFix: true,
		},
		{
			name: "complex commands no fix",
			content: `FROM alpine
RUN if true; then echo yes; fi
RUN echo 2
RUN echo 3
`,
			wantHasFix: false, // Complex command prevents fix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, nil)
			violations := rule.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			hasFix := v.SuggestedFix != nil
			if hasFix != tt.wantHasFix {
				t.Errorf("violation has fix = %v, want %v", hasFix, tt.wantHasFix)
			}
			if hasFix && v.SuggestedFix.Priority != 100 {
				t.Errorf("fix priority = %d, want 100", v.SuggestedFix.Priority)
			}
		})
	}
}

func TestPreferHeredocRule_ValidateConfig(t *testing.T) {
	t.Parallel()
	rule := NewPreferHeredocRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: false,
		},
		{
			name: "valid config",
			config: map[string]any{
				"min-commands":           4,
				"check-consecutive-runs": false,
				"check-chained-commands": true,
			},
			wantErr: false,
		},
		{
			name: "invalid min-commands below minimum",
			config: map[string]any{
				"min-commands": 1,
			},
			wantErr: true,
		},
		{
			name: "invalid additional property",
			config: map[string]any{
				"unknown-field": true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := rule.ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFormatHeredocWithMounts(t *testing.T) {
	t.Parallel()
	commands := []string{"apt-get update", "apt-get install -y vim", "apt-get clean"}

	t.Run("without mounts", func(t *testing.T) {
		t.Parallel()
		result := heredoc.FormatWithMounts(commands, nil, shell.VariantBash, false)

		expected := "RUN <<EOF\nset -e\napt-get update\napt-get install -y vim\napt-get clean\nEOF"

		if result != expected {
			t.Errorf("heredoc.FormatWithMounts() =\n%s\nwant:\n%s", result, expected)
		}
	})

	t.Run("with cache mount", func(t *testing.T) {
		t.Parallel()
		mounts := []*instructions.Mount{{
			Type:   instructions.MountTypeCache,
			Target: "/var/cache/apt",
		}}
		result := heredoc.FormatWithMounts(commands, mounts, shell.VariantBash, false)

		expected := "RUN --mount=type=cache,target=/var/cache/apt <<EOF\nset -e\napt-get update\napt-get install -y vim\napt-get clean\nEOF"

		if result != expected {
			t.Errorf("heredoc.FormatWithMounts() =\n%s\nwant:\n%s", result, expected)
		}
	})

	t.Run("with multiple mounts", func(t *testing.T) {
		t.Parallel()
		mounts := []*instructions.Mount{
			{Type: instructions.MountTypeCache, Target: "/var/cache/apt"},
			{Type: instructions.MountTypeCache, Target: "/root/.cache"},
		}
		result := heredoc.FormatWithMounts(commands, mounts, shell.VariantBash, false)

		expected := "RUN --mount=type=cache,target=/var/cache/apt " +
			"--mount=type=cache,target=/root/.cache " +
			"<<EOF\nset -e\napt-get update\napt-get install -y vim\napt-get clean\nEOF"

		if result != expected {
			t.Errorf("heredoc.FormatWithMounts() =\n%s\nwant:\n%s", result, expected)
		}
	})
}

func TestFormatHeredocWithPipefail(t *testing.T) {
	t.Parallel()

	t.Run("pipefail adds set -o pipefail", func(t *testing.T) {
		t.Parallel()
		commands := []string{"apt-get update", "curl -s https://example.com | bash", "apt-get clean"}
		result := heredoc.FormatWithMounts(commands, nil, shell.VariantBash, true)

		expected := "RUN <<EOF\nset -e\nset -o pipefail\napt-get update\ncurl -s https://example.com | bash\napt-get clean\nEOF"
		if result != expected {
			t.Errorf("FormatWithMounts(pipefail=true) =\n%s\nwant:\n%s", result, expected)
		}
	})

	t.Run("pipefail false omits set -o pipefail", func(t *testing.T) {
		t.Parallel()
		commands := []string{"apt-get update", "curl -s https://example.com | bash"}
		result := heredoc.FormatWithMounts(commands, nil, shell.VariantBash, false)

		expected := "RUN <<EOF\nset -e\napt-get update\ncurl -s https://example.com | bash\nEOF"
		if result != expected {
			t.Errorf("FormatWithMounts(pipefail=false) =\n%s\nwant:\n%s", result, expected)
		}
	})

	t.Run("pipefail deduplicates bare set -o pipefail", func(t *testing.T) {
		t.Parallel()
		commands := []string{"set -o pipefail", "curl -s https://example.com | bash"}
		result := heredoc.FormatWithMounts(commands, nil, shell.VariantBash, true)

		expected := "RUN <<EOF\nset -e\nset -o pipefail\ncurl -s https://example.com | bash\nEOF"
		if result != expected {
			t.Errorf("FormatWithMounts(pipefail dedup) =\n%s\nwant:\n%s", result, expected)
		}
	})

	t.Run("pipefail preserves set -euo pipefail", func(t *testing.T) {
		t.Parallel()
		commands := []string{"set -euo pipefail", "curl -s https://example.com | bash"}
		result := heredoc.FormatWithMounts(commands, nil, shell.VariantBash, true)

		// "set -euo pipefail" sets additional flags (-u), so it's preserved
		expected := "RUN <<EOF\nset -e\nset -o pipefail\nset -euo pipefail\ncurl -s https://example.com | bash\nEOF"
		if result != expected {
			t.Errorf("FormatWithMounts(preserve -euo) =\n%s\nwant:\n%s", result, expected)
		}
	})
}

// Note: MountsEqual is now tested in the runmount package
