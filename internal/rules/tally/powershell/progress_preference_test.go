package powershell

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestProgressPreferenceRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewProgressPreferenceRule().Metadata())
}

func TestProgressPreferenceRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewProgressPreferenceRule(), []testutil.RuleTestCase{
		// === No violations ===
		{
			Name: "RUN without Invoke-WebRequest",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force
`,
			WantViolations: 0,
		},
		{
			Name: "SHELL prelude already sets $ProgressPreference",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ProgressPreference = 'SilentlyContinue';"]
RUN Invoke-WebRequest https://example.com/file.zip -OutFile /tmp/file.zip
`,
			WantViolations: 0,
		},
		{
			Name: "script prelude sets $ProgressPreference before IWR",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN $ProgressPreference = 'SilentlyContinue'; Invoke-WebRequest https://example.com/file.zip -OutFile /tmp/file.zip
`,
			WantViolations: 0,
		},
		{
			Name: "case-insensitive preference value",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN $ProgressPreference = 'silentlycontinue'; Invoke-WebRequest https://example.com -OutFile /tmp/f.zip
`,
			WantViolations: 0,
		},
		{
			Name: "bash stage without PowerShell wrapper",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
`,
			WantViolations: 0,
		},
		{
			Name: "SHELL prelude with full prefer-shell-instruction prelude",
			Content: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				`SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; ` +
				`$PSNativeCommandUseErrorActionPreference = $true; ` +
				`$ProgressPreference = 'SilentlyContinue';"]` + "\n" +
				"RUN Invoke-WebRequest https://example.com -OutFile /tmp/f.zip\n",
			WantViolations: 0,
		},
		{
			Name: "heredoc body already sets preference",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN <<EOF
$ProgressPreference = 'SilentlyContinue'
Invoke-WebRequest https://example.com -OutFile /tmp/f.zip
EOF
`,
			WantViolations: 0,
		},

		// === Violations ===
		{
			Name: "plain IWR in PowerShell SHELL stage",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Invoke-WebRequest https://example.com/file.zip -OutFile /tmp/file.zip
`,
			WantViolations: 1,
		},
		{
			Name: "iwr alias in PowerShell SHELL stage",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN iwr https://example.com/file.zip -OutFile /tmp/file.zip
`,
			WantViolations: 1,
		},
		{
			Name: "assignment after command is not a valid prelude",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Invoke-WebRequest https://example.com -OutFile /tmp/f.zip; $ProgressPreference = 'SilentlyContinue'
`,
			WantViolations: 1,
		},
		{
			Name: "wrong preference value still triggers",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN $ProgressPreference = 'Continue'; Invoke-WebRequest https://example.com -OutFile /tmp/f.zip
`,
			WantViolations: 1,
		},
		{
			Name: "explicit powershell -Command wrapper inside bash stage",
			Content: `FROM ubuntu:22.04
RUN pwsh -Command "Invoke-WebRequest https://example.com -OutFile /tmp/f.zip"
`,
			WantViolations: 1,
		},
		{
			Name: "windows servercore with plain Invoke-WebRequest",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command "Invoke-WebRequest https://example.com/setup.exe -OutFile C:\setup.exe"
`,
			WantViolations: 1,
		},
		{
			Name: "multiple RUNs in same PowerShell SHELL scope each report",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Invoke-WebRequest https://example.com/a.zip -OutFile /tmp/a.zip
RUN Invoke-WebRequest https://example.com/b.zip -OutFile /tmp/b.zip
`,
			WantViolations: 2,
		},
		{
			Name: "heredoc body missing preference",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN <<EOF
Invoke-WebRequest https://example.com -OutFile /tmp/f.zip
EOF
`,
			WantViolations: 1,
		},
		{
			Name: "SHELL has partial prelude (Stop) but missing Progress",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop';"]
RUN Invoke-WebRequest https://example.com -OutFile /tmp/f.zip
`,
			WantViolations: 1,
		},
		{
			// Stage defaults to PowerShell via image but has no SHELL — Shape C.
			Name: "PowerShell stage without SHELL instruction",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
RUN Invoke-WebRequest https://example.com -OutFile /tmp/f.zip
`,
			WantViolations: 1,
		},
	})
}

func TestProgressPreferenceRule_MultiLineShellNoFix(t *testing.T) {
	t.Parallel()

	// A SHELL instruction spanning multiple physical lines via backslash
	// continuation. buildShellLineFix bails out because its bracket/quote
	// search only reads the first line and can produce invalid edits. The
	// violation is still reported, but with no SuggestedFix attached.
	content := "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
		"SHELL [\\\n" +
		"    \"pwsh\", \"-Command\"]\n" +
		"RUN Invoke-WebRequest https://example.com -OutFile /tmp/f.zip\n"

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewProgressPreferenceRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Errorf("expected no SuggestedFix on multi-line SHELL, got %+v", violations[0].SuggestedFix)
	}
}

func TestProgressPreferenceRule_OverlapWithPreferShellInstruction(t *testing.T) {
	t.Parallel()

	// When prefer-shell-instruction has applied its fix, the SHELL carries
	// the full prelude including $ProgressPreference = 'SilentlyContinue'.
	// progress-preference must not fire in that case.
	testutil.RunRuleTests(t, NewProgressPreferenceRule(), []testutil.RuleTestCase{
		{
			Name: "SHELL with full prefer-shell-instruction prelude",
			Content: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				`SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; ` +
				`$PSNativeCommandUseErrorActionPreference = $true; ` +
				`$ProgressPreference = 'SilentlyContinue';"]` + "\n" +
				"RUN Invoke-WebRequest https://example.com -OutFile /tmp/f.zip\n",
			WantViolations: 0,
		},
	})
}

func TestProgressPreferenceRule_OverlapWithErrorActionPreference(t *testing.T) {
	t.Parallel()

	// The two rules fire on orthogonal preferences. When the SHELL has
	// Stop + Native but no Progress, only progress-preference triggers.
	testutil.RunRuleTests(t, NewProgressPreferenceRule(), []testutil.RuleTestCase{
		{
			Name: "SHELL has error-action-preference prelude only",
			Content: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				`SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; ` +
				`$PSNativeCommandUseErrorActionPreference = $true;"]` + "\n" +
				"RUN Invoke-WebRequest https://example.com -OutFile /tmp/f.zip\n",
			WantViolations: 1,
		},
	})
}

func TestProgressPreferenceSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   bool
	}{
		{name: "empty script", script: "", want: false},
		{
			name:   "silent continue single-quoted",
			script: "$ProgressPreference = 'SilentlyContinue'; Invoke-WebRequest https://example.com",
			want:   true,
		},
		{
			name:   "silent continue double-quoted",
			script: `$ProgressPreference = "SilentlyContinue"; Invoke-WebRequest https://example.com`,
			want:   true,
		},
		{
			name:   "case insensitive variable name",
			script: "$progresspreference = 'SilentlyContinue'; Invoke-WebRequest https://example.com",
			want:   true,
		},
		{
			name:   "wrong value",
			script: "$ProgressPreference = 'Continue'; Invoke-WebRequest https://example.com",
			want:   false,
		},
		{
			name:   "assignment after command is not a prelude",
			script: "Invoke-WebRequest https://example.com; $ProgressPreference = 'SilentlyContinue'",
			want:   false,
		},
		{
			name:   "non-assignment stops walk, but earlier set counts",
			script: "$ProgressPreference = 'SilentlyContinue'; Write-Host hi; $ProgressPreference = 'Continue'",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := progressPreferenceSet(tt.script); got != tt.want {
				t.Errorf("progressPreferenceSet() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScriptUsesInvokeWebRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		script string
		want   bool
	}{
		{name: "empty", script: "", want: false},
		{name: "Invoke-WebRequest", script: "Invoke-WebRequest https://example.com", want: true},
		{name: "iwr alias", script: "iwr https://example.com", want: true},
		{name: "lowercase invoke-webrequest", script: "invoke-webrequest https://example.com", want: true},
		{name: "unrelated cmdlet", script: "Install-Module PSReadLine", want: false},
		{name: "iwr as substring only", script: "New-IwrWrapper", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := scriptUsesInvokeWebRequest(tt.script); got != tt.want {
				t.Errorf("scriptUsesInvokeWebRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}
