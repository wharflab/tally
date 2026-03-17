package shell

import "testing"

func TestParseExplicitShellInvocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		script         string
		wantShellName  string
		wantVariant    Variant
		wantScriptText string
		wantOK         bool
	}{
		{
			name:           "pwsh command",
			script:         `pwsh -NoLogo -NoProfile -Command "Write-Host hi"`,
			wantShellName:  "pwsh",
			wantVariant:    VariantPowerShell,
			wantScriptText: `"Write-Host hi"`,
			wantOK:         true,
		},
		{
			name:           "powershell short c",
			script:         `powershell -c Write-Host hi`,
			wantShellName:  "powershell",
			wantVariant:    VariantPowerShell,
			wantScriptText: `Write-Host hi`,
			wantOK:         true,
		},
		{
			name:           "cmd command mode",
			script:         `cmd /c icacls.exe C:\BuildAgent\* /grant:r Users:(OI)(CI)F`,
			wantShellName:  "cmd",
			wantVariant:    VariantCmd,
			wantScriptText: `icacls.exe C:\BuildAgent\* /grant:r Users:(OI)(CI)F`,
			wantOK:         true,
		},
		{
			name:           "quoted cmd payload",
			script:         `cmd /c 'certutil -generateSSTFromWU roots.sst && del roots.sst'`,
			wantShellName:  "cmd",
			wantVariant:    VariantCmd,
			wantScriptText: `'certutil -generateSSTFromWU roots.sst && del roots.sst'`,
			wantOK:         true,
		},
		{
			name:           "bash c",
			script:         `bash -c "echo $HOME"`,
			wantShellName:  "bash",
			wantVariant:    VariantBash,
			wantScriptText: `"echo $HOME"`,
			wantOK:         true,
		},
		{
			name:   "not a shell wrapper",
			script: `python -m pip install tox`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ParseExplicitShellInvocation(tt.script)
			if ok != tt.wantOK {
				t.Fatalf("ParseExplicitShellInvocation() ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.ShellName != tt.wantShellName {
				t.Fatalf("ShellName = %q, want %q", got.ShellName, tt.wantShellName)
			}
			if got.Variant != tt.wantVariant {
				t.Fatalf("Variant = %v, want %v", got.Variant, tt.wantVariant)
			}
			if got.Script != tt.wantScriptText {
				t.Fatalf("Script = %q, want %q", got.Script, tt.wantScriptText)
			}
		})
	}
}
