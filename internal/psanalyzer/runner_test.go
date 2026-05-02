package psanalyzer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNormalizePowerShellEnvRepairsWindowsProfileVars(t *testing.T) {
	t.Parallel()

	env := normalizePowerShellEnv("windows", []string{
		`PATH=C:\Program Files\PowerShell\7`,
		`USERPROFILE=C:\Users\tino`,
	})

	assertEnvHas(t, env, "WINDIR", `C:\WINDOWS`)
	assertEnvHas(t, env, "SystemRoot", `C:\WINDOWS`)
	assertEnvHas(t, env, "APPDATA", `C:\Users\tino\AppData\Roaming`)
	assertEnvHas(t, env, "LOCALAPPDATA", `C:\Users\tino\AppData\Local`)
}

func TestNormalizePowerShellEnvLeavesNonWindowsUntouched(t *testing.T) {
	t.Parallel()

	env := []string{"PATH=/usr/bin", "USERPROFILE=/tmp/user"}
	got := normalizePowerShellEnv("linux", env)
	if len(got) != len(env) {
		t.Fatalf("expected non-Windows env to remain unchanged, got %#v", got)
	}
}

func TestRunnerStopProcessCleansTempDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	r := &Runner{
		tempDir: tempDir,
		waitCh:  make(chan error, 1),
	}
	r.waitCh <- nil

	r.stopProcess()

	if r.tempDir != "" {
		t.Fatalf("tempDir = %q, want empty", r.tempDir)
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Fatalf("temp dir still exists after stopProcess(): %v", err)
	}
}

func TestRunnerAnalyzeWithInstalledPSScriptAnalyzer(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "js" {
		t.Skip("pwsh sidecar is not available on js")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not available")
	}
	if out, err := exec.Command("pwsh", "-NoProfile", "-NonInteractive", "-Command",
		"if (Get-Module -ListAvailable PSScriptAnalyzer) { 'yes' }").Output(); err != nil ||
		strings.TrimSpace(string(out)) != "yes" {
		t.Skip("PSScriptAnalyzer module not available")
	}

	r := NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = r.Close(closeCtx)
	}()

	diags, err := r.Analyze(ctx, AnalyzeRequest{ScriptDefinition: "Write-Host 'hi'\n"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	for _, d := range diags {
		if d.RuleName == "PSAvoidUsingWriteHost" {
			return
		}
	}
	t.Fatalf("expected PSAvoidUsingWriteHost diagnostic, got %#v", diags)
}

func TestRunnerFormatWithInstalledPSScriptAnalyzer(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "js" {
		t.Skip("pwsh sidecar is not available on js")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not available")
	}
	if out, err := exec.Command("pwsh", "-NoProfile", "-NonInteractive", "-Command",
		"if (Get-Module -ListAvailable PSScriptAnalyzer) { 'yes' }").Output(); err != nil ||
		strings.TrimSpace(string(out)) != "yes" {
		t.Skip("PSScriptAnalyzer module not available")
	}

	r := NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = r.Close(closeCtx)
	}()

	formatted, err := r.Format(ctx, FormatRequest{ScriptDefinition: "if ($true) {\nwrite-host hi\n}\n"})
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if !strings.Contains(formatted, "    write-host hi") {
		t.Fatalf("expected formatted PowerShell indentation, got:\n%s", formatted)
	}
	if strings.Contains(formatted, "Write-Host") {
		t.Fatalf("expected formatter to preserve command casing, got:\n%s", formatted)
	}
}

func TestRunnerAnalyzeForwardsRuleSettings(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("pwsh sidecar is not available on js")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not available")
	}

	homeDir := t.TempDir()
	moduleRoot := powerShellUserModuleRoot(homeDir)
	writeFakePSScriptAnalyzerModule(t, moduleRoot, `
function Invoke-ScriptAnalyzer {
    param(
        [string] $Path,
        [string] $ScriptDefinition,
        [hashtable] $Settings
    )
    if ($null -eq $Settings) {
        throw 'missing settings'
    }
    if (-not $Settings.ContainsKey('Rules')) {
        throw 'missing Rules settings'
    }
    $ruleSettings = $Settings['Rules']['PSUseCompatibleTypes']
    if ($null -eq $ruleSettings) {
        throw 'missing PSUseCompatibleTypes settings'
    }
    if (-not [bool] $ruleSettings['Enable']) {
        throw 'missing Enable setting'
    }
    $profiles = @($ruleSettings['TargetProfiles'])
    if ($profiles.Count -ne 1 -or $profiles[0] -ne 'ubuntu_x64_18.04_6.1.3_x64_4.0.30319.42000_core') {
        throw "unexpected TargetProfiles: $($profiles -join ',')"
    }
    [pscustomobject] @{
        RuleName = 'PSUseCompatibleTypes'
        Severity = 1
        Line = 1
        Column = 1
        Message = 'settings forwarded'
        ScriptPath = ''
    }
}
`)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(homeDir, ".local", "share"))
	t.Setenv("PSModulePath", "")
	t.Setenv(progressNoticeEnv, progressNoticeEnvMute)

	r := NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = r.Close(closeCtx)
	}()

	diags, err := r.Analyze(ctx, AnalyzeRequest{
		ScriptDefinition: "[System.Management.Automation.SemanticVersion]'1.18.0-rc1'\n",
		Settings: Settings{
			Rules: map[string]map[string]any{
				"PSUseCompatibleTypes": {
					"Enable": true,
					"TargetProfiles": []string{
						"ubuntu_x64_18.04_6.1.3_x64_4.0.30319.42000_core",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(diags) != 1 || diags[0].RuleName != "PSUseCompatibleTypes" {
		t.Fatalf("diagnostics = %#v, want forwarded settings diagnostic", diags)
	}
}

func TestRunnerAnalyzeReturnsSuggestedCorrections(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("pwsh sidecar is not available on js")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not available")
	}

	homeDir := t.TempDir()
	moduleRoot := powerShellUserModuleRoot(homeDir)
	writeFakePSScriptAnalyzerModule(t, moduleRoot, `
function Invoke-ScriptAnalyzer {
    param(
        [string] $Path,
        [string] $ScriptDefinition,
        [hashtable] $Settings
    )
    [pscustomobject] @{
        RuleName = 'PSAvoidUsingCmdletAliases'
        Severity = 1
        Line = 1
        Column = 1
        Message = 'alias should be expanded'
        ScriptPath = ''
        SuggestedCorrections = @(
            [pscustomobject] @{
                Description = 'Replace gci with Get-ChildItem'
                StartLineNumber = 1
                StartColumnNumber = 1
                EndLineNumber = 1
                EndColumnNumber = 4
                Text = 'Get-ChildItem'
                Lines = @('Get-ChildItem')
            }
        )
    }
}
`)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(homeDir, ".local", "share"))
	t.Setenv("PSModulePath", "")
	t.Setenv(progressNoticeEnv, progressNoticeEnvMute)

	r := NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = r.Close(closeCtx)
	}()

	diags, err := r.Analyze(ctx, AnalyzeRequest{ScriptDefinition: "gci\n"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("diagnostics = %#v, want one diagnostic", diags)
	}
	corrections := diags[0].SuggestedCorrections
	if len(corrections) != 1 {
		t.Fatalf("suggested corrections = %#v, want one correction", corrections)
	}
	got := corrections[0]
	if got.Description != "Replace gci with Get-ChildItem" ||
		got.Line != 1 ||
		got.Column != 1 ||
		got.EndLine != 1 ||
		got.EndColumn != 4 ||
		got.Text != "Get-ChildItem" {
		t.Fatalf("suggested correction = %#v", got)
	}
}

func TestSidecarBootstrapsMissingPSScriptAnalyzer(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "js" {
		t.Skip("pwsh sidecar is not available on js")
	}
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not available")
	}

	tmp := t.TempDir()
	sidecarPath := filepath.Join(tmp, "Tally.PSSA.Sidecar.ps1")
	if err := os.WriteFile(sidecarPath, sidecarScript, 0o600); err != nil {
		t.Fatal(err)
	}

	moduleRoot := filepath.Join(tmp, "modules")
	prelude := `
$ErrorActionPreference = 'Stop'
$moduleRoot = $env:TALLY_TEST_MODULE_ROOT
$env:PSModulePath = $moduleRoot
function Install-PSResource {
    [CmdletBinding()]
    param(
        [Parameter(Mandatory=$true)] [string] $Name,
        [string] $Scope,
        [string] $Version,
        [switch] $TrustRepository,
        [switch] $AcceptLicense,
        [switch] $Quiet
    )
    if ($Name -ne 'PSScriptAnalyzer') {
        throw "unexpected module: $Name"
    }
    if ($Version -ne '1.25.0') {
        throw "unexpected version: $Version"
    }
    $moduleDir = Join-Path (Join-Path $moduleRoot 'PSScriptAnalyzer') $Version
    New-Item -ItemType Directory -Force -Path $moduleDir | Out-Null
    @'
function Invoke-ScriptAnalyzer {
    param(
        [string] $Path,
        [string] $ScriptDefinition,
        [hashtable] $Settings
    )
    @()
}
'@ | Set-Content -LiteralPath (Join-Path $moduleDir 'PSScriptAnalyzer.psm1') -Encoding UTF8
    @"
@{
    RootModule = 'PSScriptAnalyzer.psm1'
    ModuleVersion = '$Version'
    GUID = 'd6245804-193d-414e-bac3-f7f51deafabb'
}
"@ | Set-Content -LiteralPath (Join-Path $moduleDir 'PSScriptAnalyzer.psd1') -Encoding UTF8
}
& $env:TALLY_TEST_SIDECAR
`

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pwsh", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", prelude)
	cmd.Env = append(normalizePowerShellEnv(runtime.GOOS, os.Environ()),
		"PSModulePath="+moduleRoot,
		"TALLY_TEST_MODULE_ROOT="+moduleRoot,
		"TALLY_TEST_SIDECAR="+sidecarPath,
	)
	cmd.Stdin = strings.NewReader("{\"id\":\"1\",\"op\":\"shutdown\"}\n")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sidecar failed: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, `"progress":true`) {
		t.Fatalf("sidecar output missing install progress event: %s", output)
	}
	if !strings.Contains(output, `"ready":true`) {
		t.Fatalf("sidecar output missing ready handshake: %s", output)
	}
	if !strings.Contains(output, `"ok":true`) {
		t.Fatalf("sidecar output missing shutdown response: %s", output)
	}
	if _, err := os.Stat(filepath.Join(moduleRoot, "PSScriptAnalyzer", "1.25.0", "PSScriptAnalyzer.psm1")); err != nil {
		t.Fatalf("fake installer did not create module: %v", err)
	}
}

func writeFakePSScriptAnalyzerModule(t *testing.T, moduleRoot, moduleScript string) {
	t.Helper()

	moduleDir := filepath.Join(moduleRoot, "PSScriptAnalyzer", "1.25.0")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "PSScriptAnalyzer.psm1"), []byte(moduleScript), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `@{
    RootModule = 'PSScriptAnalyzer.psm1'
    ModuleVersion = '1.25.0'
    GUID = 'd6245804-193d-414e-bac3-f7f51deafabb'
    FunctionsToExport = @('Invoke-ScriptAnalyzer')
}
`
	if err := os.WriteFile(filepath.Join(moduleDir, "PSScriptAnalyzer.psd1"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

func powerShellUserModuleRoot(homeDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(homeDir, "Documents", "PowerShell", "Modules")
	}
	return filepath.Join(homeDir, ".local", "share", "powershell", "Modules")
}

func assertEnvHas(t *testing.T, env []string, key, want string) {
	t.Helper()
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(k, key) {
			if v != want {
				t.Fatalf("%s = %q, want %q", key, v, want)
			}
			return
		}
	}
	t.Fatalf("missing env key %s in %#v", key, env)
}
