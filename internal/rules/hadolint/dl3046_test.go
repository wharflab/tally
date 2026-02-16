package hadolint

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDL3046Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3046Rule().Metadata())
}

func TestDL3046Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		dockerfile  string
		wantCount   int
		wantCode    string
		useSemantic bool // Use semantic model for shell variant detection
	}{
		// Test cases from original Hadolint spec
		{
			name: "ok with useradd alone",
			dockerfile: `FROM alpine:3.18
RUN useradd luser
`,
			wantCount: 0,
		},
		{
			name: "ok with useradd short uid",
			dockerfile: `FROM alpine:3.18
RUN useradd -u 12345 luser
`,
			wantCount: 0,
		},
		{
			name: "ok with useradd long uid and flag -l",
			dockerfile: `FROM alpine:3.18
RUN useradd -l -u 123456 luser
`,
			wantCount: 0,
		},
		{
			name: "ok with useradd and just flag -l",
			dockerfile: `FROM alpine:3.18
RUN useradd -l luser
`,
			wantCount: 0,
		},
		{
			name: "warn when useradd and long uid without flag -l",
			dockerfile: `FROM alpine:3.18
RUN useradd -u 123456 luser
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3046",
		},
		// Additional edge cases
		{
			name: "ok with --no-log-init long flag",
			dockerfile: `FROM alpine:3.18
RUN useradd --no-log-init -u 123456 luser
`,
			wantCount: 0,
		},
		{
			name: "ok with --uid long flag and short value",
			dockerfile: `FROM alpine:3.18
RUN useradd --uid 12345 luser
`,
			wantCount: 0,
		},
		{
			name: "warn with --uid long flag and high value",
			dockerfile: `FROM alpine:3.18
RUN useradd --uid 123456 luser
`,
			wantCount: 1,
		},
		{
			name: "ok with uid at boundary (5 digits)",
			dockerfile: `FROM alpine:3.18
RUN useradd -u 99999 luser
`,
			wantCount: 0,
		},
		{
			name: "warn with uid just over boundary (6 digits)",
			dockerfile: `FROM alpine:3.18
RUN useradd -u 100000 luser
`,
			wantCount: 1,
		},
		{
			name: "ok with very high uid but has -l flag",
			dockerfile: `FROM alpine:3.18
RUN useradd -l -u 1000000000 luser
`,
			wantCount: 0,
		},
		{
			name: "warn with very high uid (many digits)",
			dockerfile: `FROM alpine:3.18
RUN useradd -u 1000000000 luser
`,
			wantCount: 1,
		},
		{
			name: "multi-stage dockerfile",
			dockerfile: `FROM alpine:3.18 AS builder
RUN useradd -u 123456 builduser

FROM alpine:3.18
RUN useradd -l -u 123456 appuser
`,
			wantCount: 1,
		},
		{
			name: "multiple useradd commands in same RUN",
			dockerfile: `FROM alpine:3.18
RUN useradd -u 123456 user1 && useradd -u 234567 user2
`,
			wantCount: 2,
		},
		{
			name: "useradd in chained commands - one ok one warn",
			dockerfile: `FROM alpine:3.18
RUN useradd -l -u 123456 user1 && useradd -u 234567 user2
`,
			wantCount: 1,
		},
		{
			name: "exec form useradd",
			dockerfile: `FROM alpine:3.18
RUN ["useradd", "-u", "123456", "luser"]
`,
			wantCount: 1,
		},
		{
			name: "useradd with equals form --uid=123456",
			dockerfile: `FROM alpine:3.18
RUN useradd --uid=123456 luser
`,
			wantCount: 1,
		},
		{
			name: "ok with useradd --uid=123456 and -l",
			dockerfile: `FROM alpine:3.18
RUN useradd -l --uid=123456 luser
`,
			wantCount: 0,
		},
		{
			name: "useradd without uid flag should not trigger",
			dockerfile: `FROM alpine:3.18
RUN useradd -m -s /bin/bash developer
`,
			wantCount: 0,
		},
		// Non-POSIX shell stage tests (require semantic model for shell variant detection)
		{
			name: "exec form useradd in PowerShell stage still triggers",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["powershell", "-Command"]
RUN ["useradd", "-u", "123456", "luser"]
`,
			wantCount:   1,
			useSemantic: true,
		},
		{
			name: "shell form useradd in PowerShell stage is skipped",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["powershell", "-Command"]
RUN useradd -u 123456 luser
`,
			wantCount:   0, // shell-form skipped in non-POSIX stage
			useSemantic: true,
		},
		{
			name: "exec form ok with -l in PowerShell stage",
			dockerfile: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["powershell", "-Command"]
RUN ["useradd", "-l", "-u", "123456", "luser"]
`,
			wantCount:   0,
			useSemantic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var input rules.LintInput
			if tt.useSemantic {
				input = testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)
			} else {
				input = testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			}

			r := NewDL3046Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}
		})
	}
}

func TestDL3046Rule_AutoFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantFix    bool
	}{
		{
			name: "shell form gets auto-fix",
			dockerfile: `FROM alpine:3.18
RUN useradd -u 123456 luser
`,
			wantFix: true,
		},
		{
			name: "exec form does not get auto-fix",
			dockerfile: `FROM alpine:3.18
RUN ["useradd", "-u", "123456", "luser"]
`,
			wantFix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3046Rule()
			violations := r.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			hasFix := violations[0].SuggestedFix != nil
			if hasFix != tt.wantFix {
				t.Errorf("hasFix = %v, want %v", hasFix, tt.wantFix)
			}

			if hasFix && violations[0].SuggestedFix != nil {
				fix := violations[0].SuggestedFix
				if fix.Description != "Add -l flag to useradd" {
					t.Errorf("fix description = %q, want %q", fix.Description, "Add -l flag to useradd")
				}
				if fix.Safety != rules.FixSafe {
					t.Errorf("fix safety = %v, want FixSafe", fix.Safety)
				}
				if len(fix.Edits) != 1 {
					t.Errorf("fix has %d edits, want 1", len(fix.Edits))
				} else if fix.Edits[0].NewText != " -l" {
					t.Errorf("fix newText = %q, want %q", fix.Edits[0].NewText, " -l")
				}
			}
		})
	}
}

func TestIsHighUIDWithoutNoLogInit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{
			name: "no uid flag",
			args: []string{"luser"},
			want: false,
		},
		{
			name: "short uid",
			args: []string{"-u", "12345", "luser"},
			want: false,
		},
		{
			name: "short uid at boundary",
			args: []string{"-u", "99999", "luser"},
			want: false,
		},
		{
			name: "long uid without -l",
			args: []string{"-u", "123456", "luser"},
			want: true,
		},
		{
			name: "long uid with -l",
			args: []string{"-l", "-u", "123456", "luser"},
			want: false,
		},
		{
			name: "long uid with --no-log-init",
			args: []string{"--no-log-init", "-u", "123456", "luser"},
			want: false,
		},
		{
			name: "long uid with --uid",
			args: []string{"--uid", "123456", "luser"},
			want: true,
		},
		{
			name: "long uid with --uid=value",
			args: []string{"--uid=123456", "luser"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd := &shell.CommandInfo{
				Name: "useradd",
				Args: tt.args,
			}
			if tt.args[0] != "-l" && tt.args[0] != "--no-log-init" && !strings.HasPrefix(tt.args[0], "-") {
				cmd.Subcommand = tt.args[0]
			}
			got := isHighUIDWithoutNoLogInit(cmd)
			if got != tt.want {
				t.Errorf("isHighUIDWithoutNoLogInit() = %v, want %v", got, tt.want)
			}
		})
	}
}
