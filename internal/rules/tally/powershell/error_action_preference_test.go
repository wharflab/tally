package powershell

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestErrorActionPreferenceRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewErrorActionPreferenceRule().Metadata())
}

func TestErrorActionPreferenceRule_DefaultConfig(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, DefaultErrorActionPreferenceConfig())
}

func TestErrorActionPreferenceRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewErrorActionPreferenceRule(), []testutil.RuleTestCase{
		// === No violations ===
		{
			Name: "single-command powershell RUN (default min-statements=2)",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force
`,
			WantViolations: 0,
		},
		{
			Name: "non-powershell stage",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
`,
			WantViolations: 0,
		},
		{
			Name: "SHELL with full prelude and multi-command RUN",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"]
RUN Install-Module PSReadLine -Force; Write-Host done
`,
			WantViolations: 0,
		},
		{
			Name: "script body includes both preludes",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN $ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true; Install-Module PSReadLine -Force; Write-Host done
`,
			WantViolations: 0,
		},
		{
			Name: "explicit wrapper with both preludes in script",
			Content: "FROM ubuntu:22.04\n" +
				"RUN pwsh -Command \"$ErrorActionPreference = 'Stop'; " +
				"$PSNativeCommandUseErrorActionPreference = $true; " +
				"Install-Module PSReadLine -Force; Write-Host done\"\n",
			WantViolations: 0,
		},
		{
			Name: "single explicit wrapper (below default threshold)",
			Content: `FROM ubuntu:22.04
RUN pwsh -Command Install-Module PSReadLine -Force
`,
			WantViolations: 0,
		},

		// === Violations ===
		{
			Name: "multi-command PS RUN missing both preludes",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force; Write-Host done
`,
			WantViolations: 1,
		},
		{
			Name: "SHELL without prelude and multi-command RUN",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force; Invoke-WebRequest https://example.com -OutFile /tmp/file.zip
`,
			WantViolations: 1,
		},
		{
			Name: "explicit pwsh wrapper with multi-statement script",
			Content: `FROM ubuntu:22.04
RUN pwsh -Command "Install-Module PSReadLine -Force; Write-Host done"
`,
			WantViolations: 1,
		},
		{
			Name: "linux stage with pwsh multi-commands (cross-platform)",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force; Install-Module Az -Force
`,
			WantViolations: 1,
		},
		{
			Name: "ErrorActionPreference set to Continue (wrong value)",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN $ErrorActionPreference = 'Continue'; Install-Module PSReadLine -Force; Write-Host done
`,
			WantViolations: 1,
		},
		{
			Name: "has Stop but missing PSNativeCommandUseErrorActionPreference",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN $ErrorActionPreference = 'Stop'; Install-Module PSReadLine -Force; Write-Host done
`,
			WantViolations: 1,
		},
		{
			Name: "SHELL has Stop but no native preference",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop';"]
RUN Install-Module PSReadLine -Force; Write-Host done
`,
			WantViolations: 1,
		},
		{
			Name: "multiple RUNs in same stage each trigger",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force; Write-Host one
RUN Install-Module Az -Force; Write-Host two
`,
			WantViolations: 2,
		},
		{
			Name: "windows stage with powershell wrapper",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command "Install-Module PSReadLine -Force; Write-Host done"
`,
			WantViolations: 1,
		},
	})
}

func TestErrorActionPreferenceRule_CheckWithConfig(t *testing.T) {
	t.Parallel()

	minOne := 1
	testutil.RunRuleTests(t, NewErrorActionPreferenceRule(), []testutil.RuleTestCase{
		{
			Name: "single-command with min-statements=1 triggers",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force
`,
			Config:         &ErrorActionPreferenceConfig{MinStatements: &minOne},
			WantViolations: 1,
		},
		{
			Name: "single-command with min-statements=1 and prelude is clean",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"]
RUN Install-Module PSReadLine -Force
`,
			Config:         &ErrorActionPreferenceConfig{MinStatements: &minOne},
			WantViolations: 0,
		},
	})
}

func TestErrorActionPreferenceRule_OverlapWithPreferShellInstruction(t *testing.T) {
	t.Parallel()

	// When prefer-shell-instruction's fix has been applied, the SHELL
	// instruction carries the full prelude. error-action-preference must
	// not fire in this case.
	testutil.RunRuleTests(t, NewErrorActionPreferenceRule(), []testutil.RuleTestCase{
		{
			Name: "SHELL with full prelude from prefer-shell-instruction fix",
			Content: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				`SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; ` +
				`$PSNativeCommandUseErrorActionPreference = $true; ` +
				`$ProgressPreference = 'SilentlyContinue';"]` + "\n" +
				"RUN Install-Module PSReadLine -Force; Write-Host done\n",
			WantViolations: 0,
		},
		{
			Name: "SHELL with partial prelude still triggers for missing native",
			Content: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				`SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop';"]` + "\n" +
				"RUN Install-Module PSReadLine -Force; Write-Host done\n",
			WantViolations: 1,
		},
	})
}

func TestErrorActionPreferenceRule_OverlapWithPreferRunHeredoc(t *testing.T) {
	t.Parallel()

	// prefer-run-heredoc converts multi-statement RUNs to heredoc syntax and
	// injects the prelude in the heredoc body. However, rules check the
	// original source, so error-action-preference should still fire on the
	// pre-heredoc form (heredoc conversion happens at fix time, not detection).
	testutil.RunRuleTests(t, NewErrorActionPreferenceRule(), []testutil.RuleTestCase{
		{
			Name: "multi-command RUN eligible for heredoc still triggers",
			Content: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				`SHELL ["pwsh", "-Command"]` + "\n" +
				"RUN Install-Module PSReadLine -Force; Write-Host done\n",
			WantViolations: 1,
		},
		{
			Name: "heredoc RUN with prelude already present does not trigger",
			Content: "FROM mcr.microsoft.com/powershell:ubuntu-22.04\n" +
				`SHELL ["pwsh", "-Command"]` + "\n" +
				"RUN <<EOF\n" +
				"$ErrorActionPreference = 'Stop'\n" +
				"$PSNativeCommandUseErrorActionPreference = $true\n" +
				"Install-Module PSReadLine -Force\n" +
				"Write-Host done\n" +
				"EOF\n",
			WantViolations: 0,
		},
	})
}

func TestShellPreludeState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		shellArgs  []string
		wantStop   bool
		wantNative bool
	}{
		{
			name:       "nil shell args",
			shellArgs:  nil,
			wantStop:   false,
			wantNative: false,
		},
		{
			name:       "just executable",
			shellArgs:  []string{"pwsh"},
			wantStop:   false,
			wantNative: false,
		},
		{
			name:       "only -Command flag",
			shellArgs:  []string{"pwsh", "-Command"},
			wantStop:   false,
			wantNative: false,
		},
		{
			name:       "full prelude",
			shellArgs:  []string{"pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"},
			wantStop:   true,
			wantNative: true,
		},
		{
			name:       "only Stop",
			shellArgs:  []string{"pwsh", "-Command", "$ErrorActionPreference = 'Stop';"},
			wantStop:   true,
			wantNative: false,
		},
		{
			name:       "only native",
			shellArgs:  []string{"pwsh", "-Command", "$PSNativeCommandUseErrorActionPreference = $true;"},
			wantStop:   false,
			wantNative: true,
		},
		{
			name: "false positive: Stop-Process not ErrorActionPreference",
			shellArgs: []string{
				"pwsh", "-Command",
				"$ErrorActionPreference = 'Continue'; Stop-Process -Name foo",
			},
			wantStop:   false,
			wantNative: false,
		},
		{
			name: "false positive: unrelated $true not native preference",
			shellArgs: []string{
				"pwsh", "-Command",
				"$ErrorActionPreference = 'Stop'; $someFlag = $true",
			},
			wantStop:   true,
			wantNative: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotStop, gotNative := shellPreludeStateFromCmd(tt.shellArgs)
			if gotStop != tt.wantStop {
				t.Errorf("shellPreludeState() stop = %v, want %v", gotStop, tt.wantStop)
			}
			if gotNative != tt.wantNative {
				t.Errorf("shellPreludeState() native = %v, want %v", gotNative, tt.wantNative)
			}
		})
	}
}

func TestScanScriptPrelude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		script        string
		wantStop      bool
		wantNative    bool
		wantWrongStop bool
	}{
		{
			name:   "no prelude",
			script: "Install-Module PSReadLine -Force; Write-Host done",
		},
		{
			name:     "has Stop",
			script:   "$ErrorActionPreference = 'Stop'; Install-Module PSReadLine -Force",
			wantStop: true,
		},
		{
			name:       "has native",
			script:     "$PSNativeCommandUseErrorActionPreference = $true; Install-Module PSReadLine -Force",
			wantNative: true,
		},
		{
			name:       "has both",
			script:     "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true; Install-Module PSReadLine -Force",
			wantStop:   true,
			wantNative: true,
		},
		{
			name:          "wrong Stop value",
			script:        "$ErrorActionPreference = 'Continue'; Install-Module PSReadLine -Force",
			wantWrongStop: true,
		},
		{
			name:   "assignment after command is not prelude",
			script: "Invoke-WebRequest https://example.com; $ErrorActionPreference = 'Stop'",
		},
		{
			name:   "native after command is not prelude",
			script: "Write-Host hi; $PSNativeCommandUseErrorActionPreference = $true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotStop, gotNative, gotWrongStop := scanScriptPrelude(tt.script)
			if gotStop != tt.wantStop {
				t.Errorf("scanScriptPrelude() stop = %v, want %v", gotStop, tt.wantStop)
			}
			if gotNative != tt.wantNative {
				t.Errorf("scanScriptPrelude() native = %v, want %v", gotNative, tt.wantNative)
			}
			if gotWrongStop != tt.wantWrongStop {
				t.Errorf("scanScriptPrelude() wrongStop = %v, want %v", gotWrongStop, tt.wantWrongStop)
			}
		})
	}
}

func TestBuildViolationMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		hasStop      bool
		hasNative    bool
		hasWrongStop bool
		wantMsg      string
	}{
		{
			name:    "both missing",
			wantMsg: "PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true",
		},
		{
			name:         "both missing with wrong Stop",
			hasWrongStop: true,
			wantMsg:      "PowerShell RUN sets $ErrorActionPreference to a non-Stop value and is missing $PSNativeCommandUseErrorActionPreference",
		},
		{
			name:      "only Stop missing",
			hasNative: true,
			wantMsg:   "PowerShell RUN is missing $ErrorActionPreference = 'Stop'",
		},
		{
			name:    "only native missing",
			hasStop: true,
			wantMsg: "PowerShell RUN is missing $PSNativeCommandUseErrorActionPreference = $true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotMsg, gotDetail := buildViolationMessage(tt.hasStop, tt.hasNative, tt.hasWrongStop)
			if gotMsg != tt.wantMsg {
				t.Errorf("got message %q, want %q", gotMsg, tt.wantMsg)
			}
			if gotDetail == "" {
				t.Error("expected non-empty detail")
			}
		})
	}
}
