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
        [switch] $TrustRepository,
        [switch] $AcceptLicense,
        [switch] $Quiet
    )
    if ($Name -ne 'PSScriptAnalyzer') {
        throw "unexpected module: $Name"
    }
    $moduleDir = Join-Path $moduleRoot 'PSScriptAnalyzer'
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
	if _, err := os.Stat(filepath.Join(moduleRoot, "PSScriptAnalyzer", "PSScriptAnalyzer.psm1")); err != nil {
		t.Fatalf("fake installer did not create module: %v", err)
	}
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
