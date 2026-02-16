package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestDL4006Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL4006Rule().Metadata())
}

func TestDL4006Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		// --- Cases from Hadolint spec (ruleCatches → violation expected) ---
		{
			name: "warn on missing pipefail",
			dockerfile: `FROM scratch
RUN wget -O - https://some.site | wc -l > /number
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL4006",
		},
		{
			name: "warns when using plain sh",
			dockerfile: `FROM scratch as build
SHELL ["/bin/sh", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l file > /number
`,
			wantCount: 1,
		},
		{
			name: "warn on missing pipefail in the next image",
			dockerfile: `FROM scratch as build
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l file > /number
FROM scratch as build2
RUN wget -O - https://some.site | wc -l file > /number
`,
			wantCount: 1,
		},
		{
			name: "warn on missing pipefail if next SHELL is not using it",
			dockerfile: `FROM scratch as build
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l file > /number
SHELL ["/bin/sh", "-c"]
RUN wget -O - https://some.site | wc -l file > /number
`,
			wantCount: 1,
		},

		// --- Cases from Hadolint spec (ruleCatchesNot → no violation) ---
		{
			name: "don't warn on commands with no pipes",
			dockerfile: `FROM scratch as build
RUN wget -O - https://some.site && wc -l file > /number
`,
			wantCount: 0,
		},
		{
			name: "don't warn on commands with pipes and the pipefail option",
			dockerfile: `FROM scratch as build
SHELL ["/bin/bash", "-eo", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l file > /number
`,
			wantCount: 0,
		},
		{
			name: "don't warn on commands with pipes and the pipefail option 2",
			dockerfile: `FROM scratch as build
SHELL ["/bin/bash", "-e", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l file > /number
`,
			wantCount: 0,
		},
		{
			name: "don't warn on commands with pipes and the pipefail option 3",
			dockerfile: `FROM scratch as build
SHELL ["/bin/bash", "-o", "errexit", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l file > /number
`,
			wantCount: 0,
		},
		{
			name: "don't warn on commands with pipes and the pipefail zsh",
			dockerfile: `FROM scratch as build
SHELL ["/bin/zsh", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l file > /number
`,
			wantCount: 0,
		},
		{
			name: "don't warn on powershell",
			dockerfile: `FROM scratch as build
SHELL ["pwsh", "-c"]
RUN Get-Variable PSVersionTable | Select-Object -ExpandProperty Value
`,
			wantCount: 0,
		},
		{
			name: "ignore non posix shells: pwsh",
			dockerfile: `FROM mcr.microsoft.com/powershell:ubuntu-16.04
SHELL [ "pwsh", "-c" ]
RUN Get-Variable PSVersionTable | Select-Object -ExpandProperty Value
`,
			wantCount: 0,
		},
		{
			name: "ignore non posix shells: powershell",
			dockerfile: `FROM mcr.microsoft.com/powershell:ubuntu-16.04
SHELL [ "powershell.exe" ]
RUN Get-Variable PSVersionTable | Select-Object -ExpandProperty Value
`,
			wantCount: 0,
		},
		{
			name: "ignore non posix shells: cmd.exe",
			dockerfile: `FROM mcr.microsoft.com/powershell:ubuntu-16.04
SHELL [ "cmd.exe", "/c" ]
RUN Get-Variable PSVersionTable | Select-Object -ExpandProperty Value
`,
			wantCount: 0,
		},

		{
			name: "ignore non posix shells: Windows backslash path",
			dockerfile: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"SHELL [\"C:\\\\Windows\\\\System32\\\\WindowsPowerShell\\\\v1.0\\\\powershell.exe\", \"-Command\"]\n" +
				"RUN Get-Variable PSVersionTable | Select-Object -ExpandProperty Value\n",
			wantCount: 0,
		},

		// --- Additional edge cases ---
		{
			name: "no warning on exec form RUN with pipes",
			dockerfile: `FROM scratch
RUN ["sh", "-c", "wget -O - https://some.site | wc -l"]
`,
			wantCount: 0,
		},
		{
			name: "pipefail with ash shell",
			dockerfile: `FROM scratch
SHELL ["/bin/ash", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l
`,
			wantCount: 0,
		},
		{
			name: "multiple RUN with pipes all flagged without pipefail",
			dockerfile: `FROM scratch
RUN wget -O - https://some.site | wc -l
RUN curl -s https://example.com | grep test
`,
			wantCount: 2,
		},
		{
			name: "pipe in subshell",
			dockerfile: `FROM scratch
RUN (wget -O - https://some.site | wc -l)
`,
			wantCount: 1,
		},
		{
			name: "pipe after && chain",
			dockerfile: `FROM scratch
RUN apt-get update && apt-get install -y curl | tee /log
`,
			wantCount: 1,
		},
		{
			name: "no pipe just redirection",
			dockerfile: `FROM scratch
RUN echo hello > /file
`,
			wantCount: 0,
		},
		{
			name: "pipefail persists after SHELL until next FROM",
			dockerfile: `FROM scratch
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l
RUN curl -s https://example.com | grep test
`,
			wantCount: 0,
		},
		{
			name: "SHELL resets after new FROM even with same name",
			dockerfile: `FROM scratch as build
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l
FROM scratch as build2
RUN curl -s https://example.com | grep test
`,
			wantCount: 1,
		},
		{
			name: "SHELL without pipefail after SHELL with pipefail",
			dockerfile: `FROM scratch
SHELL ["/bin/bash", "-o", "pipefail", "-c"]
RUN wget -O - https://some.site | wc -l
SHELL ["/bin/bash", "-c"]
RUN curl -s https://example.com | grep test
`,
			wantCount: 1,
		},
		{
			name: "no RUN instructions",
			dockerfile: `FROM scratch
COPY . /app
`,
			wantCount: 0,
		},
		{
			name: "RUN without pipes",
			dockerfile: `FROM scratch
RUN echo hello
`,
			wantCount: 0,
		},
		{
			name: "multiline RUN with pipe",
			dockerfile: `FROM scratch
RUN wget -O - \
    https://some.site \
    | wc -l > /number
`,
			wantCount: 1,
		},
		{
			name: "piped RUN before later SHELL pwsh still triggers",
			dockerfile: `FROM scratch
RUN wget -O - https://some.site | wc -l > /number
SHELL ["pwsh", "-c"]
RUN Get-Variable PSVersionTable | Select-Object -ExpandProperty Value
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL4006",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)

			r := NewDL4006Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s at line %d: %s", v.RuleCode, v.Line(), v.Message)
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

func TestDL4006Rule_Fix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantFix    bool
	}{
		{
			name: "fix suggests SHELL instruction",
			dockerfile: `FROM scratch
RUN wget -O - https://some.site | wc -l > /number
`,
			wantFix: true,
		},
		{
			name: "no fix for exec form",
			dockerfile: `FROM scratch
RUN ["sh", "-c", "wget -O - https://some.site | wc -l"]
`,
			wantFix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)

			r := NewDL4006Rule()
			violations := r.Check(input)

			if tt.wantFix {
				if len(violations) == 0 {
					t.Fatal("expected a violation but got none")
				}
				if violations[0].SuggestedFix == nil {
					t.Error("expected a suggested fix but got none")
				}
			}

			if !tt.wantFix && len(violations) > 0 && violations[0].SuggestedFix != nil {
				t.Error("expected no fix but got one")
			}
		})
	}
}

func TestDL4006Rule_HeredocCoordination(t *testing.T) {
	t.Parallel()

	t.Run("skip fix when prefer-run-heredoc is enabled and command is heredoc candidate", func(t *testing.T) {
		t.Parallel()
		// A heredoc candidate has multiple chained commands
		input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", `FROM scratch
RUN apt-get update && apt-get install -y curl && curl -s https://some.site | wc -l
`)
		input.EnabledRules = []string{"tally/prefer-run-heredoc"}

		r := NewDL4006Rule()
		violations := r.Check(input)

		if len(violations) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(violations))
		}
		// Fix should be skipped because heredoc would handle this
		if violations[0].SuggestedFix != nil {
			t.Error("expected no fix when prefer-run-heredoc is enabled and command is heredoc candidate")
		}
	})

	t.Run("keep fix when prefer-run-heredoc is enabled but command is not heredoc candidate", func(t *testing.T) {
		t.Parallel()
		// Simple pipe command is not a heredoc candidate (not enough chained commands)
		input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", `FROM scratch
RUN wget -O - https://some.site | wc -l
`)
		input.EnabledRules = []string{"tally/prefer-run-heredoc"}

		r := NewDL4006Rule()
		violations := r.Check(input)

		if len(violations) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(violations))
		}
		// Fix should be present because it's not a heredoc candidate
		if violations[0].SuggestedFix == nil {
			t.Error("expected a fix for non-heredoc-candidate command even with prefer-run-heredoc enabled")
		}
	})
}

func TestHasPipefailOption(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		shellCmd []string
		want     bool
	}{
		{
			name:     "bash with -o pipefail",
			shellCmd: []string{"/bin/bash", "-o", "pipefail", "-c"},
			want:     true,
		},
		{
			name:     "bash with -eo pipefail",
			shellCmd: []string{"/bin/bash", "-eo", "pipefail", "-c"},
			want:     true,
		},
		{
			name:     "bash with -e -o pipefail",
			shellCmd: []string{"/bin/bash", "-e", "-o", "pipefail", "-c"},
			want:     true,
		},
		{
			name:     "bash with multiple -o options",
			shellCmd: []string{"/bin/bash", "-o", "errexit", "-o", "pipefail", "-c"},
			want:     true,
		},
		{
			name:     "zsh with -o pipefail",
			shellCmd: []string{"/bin/zsh", "-o", "pipefail", "-c"},
			want:     true,
		},
		{
			name:     "ash with -o pipefail",
			shellCmd: []string{"/bin/ash", "-o", "pipefail", "-c"},
			want:     true,
		},
		{
			name:     "sh with -o pipefail is NOT valid",
			shellCmd: []string{"/bin/sh", "-o", "pipefail", "-c"},
			want:     false,
		},
		{
			name:     "bash without pipefail",
			shellCmd: []string{"/bin/bash", "-c"},
			want:     false,
		},
		{
			name:     "bash with -o errexit only",
			shellCmd: []string{"/bin/bash", "-o", "errexit", "-c"},
			want:     false,
		},
		{
			name:     "empty shell command",
			shellCmd: []string{},
			want:     false,
		},
		{
			name:     "just shell name",
			shellCmd: []string{"/bin/bash"},
			want:     false,
		},
		{
			name:     "pwsh is not valid",
			shellCmd: []string{"pwsh", "-o", "pipefail"},
			want:     false,
		},
		{
			name:     "cmd.exe is not valid",
			shellCmd: []string{"cmd.exe", "/c"},
			want:     false,
		},
		{
			name:     "bare bash with -o pipefail",
			shellCmd: []string{"bash", "-o", "pipefail", "-c"},
			want:     true,
		},
		{
			name:     "Windows backslash path powershell is not valid",
			shellCmd: []string{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, "-o", "pipefail"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasPipefailOption(tt.shellCmd)
			if got != tt.want {
				t.Errorf("hasPipefailOption(%v) = %v, want %v", tt.shellCmd, got, tt.want)
			}
		})
	}
}
