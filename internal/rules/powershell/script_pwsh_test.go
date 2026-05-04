package powershell

import (
	json "encoding/json/v2"
	"os/exec"
	"strings"
	"testing"
)

const powerShellCommandLineOracleMarker = "Write-Output __TALLY_PWSH_ORACLE__"

type powerShellCommandLineOracleCase struct {
	Token string   `json:"token"`
	Shape string   `json:"shape"`
	Args  []string `json:"args"`
}

func TestPowerShellInvocationParserMatchesPwshCommandLineParser(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh not available")
	}

	cases := loadPowerShellCommandLineOracleCases(t)
	if len(cases) < 40 {
		t.Fatalf("pwsh oracle returned %d cases, want at least 40", len(cases))
	}

	for _, tt := range cases {
		t.Run(tt.Shape+"/"+tt.Token, func(t *testing.T) {
			t.Parallel()

			args := append([]string{"pwsh"}, tt.Args...)
			execInvocation, ok := parseExecFormPowerShellInvocation(args)
			if !ok || execInvocation.script != powerShellCommandLineOracleMarker {
				t.Fatalf(
					"exec-form parse = (%q, %v), want %q for argv %#v",
					execInvocation.script,
					ok,
					powerShellCommandLineOracleMarker,
					args,
				)
			}

			shellInvocation, ok := parseExplicitPowerShellInvocation(shellPowerShellInvocation(tt.Args))
			if !ok || shellInvocation.script != powerShellCommandLineOracleMarker {
				t.Fatalf(
					"shell-form parse = (%q, %v), want %q for args %#v",
					shellInvocation.script,
					ok,
					powerShellCommandLineOracleMarker,
					tt.Args,
				)
			}
		})
	}
}

func TestPowerShellInvocationParserSkipsTerminalSwitches(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"pwsh", "-Help", "-Command", powerShellCommandLineOracleMarker},
		{"pwsh", "-?", "-Command", powerShellCommandLineOracleMarker},
		{"pwsh", "-Version", "7.4", "-Command", powerShellCommandLineOracleMarker},
		{"pwsh", "-v", "7.4", "-Command", powerShellCommandLineOracleMarker},
	} {
		if invocation, ok := parseExecFormPowerShellInvocation(args); ok {
			t.Fatalf("parseExecFormPowerShellInvocation(%#v) = %#v, true; want false", args, invocation)
		}

		shellScript := shellPowerShellInvocation(args[1:])
		if invocation, ok := parseExplicitPowerShellInvocation(shellScript); ok {
			t.Fatalf("parseExplicitPowerShellInvocation(%q) = %#v, true; want false", shellScript, invocation)
		}
	}
}

func loadPowerShellCommandLineOracleCases(t *testing.T) []powerShellCommandLineOracleCase {
	t.Helper()

	cmd := exec.Command(
		"pwsh",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		powerShellCommandLineOracleScript,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run pwsh command-line parser oracle: %v\n%s", err, out)
	}

	var cases []powerShellCommandLineOracleCase
	if err := json.Unmarshal(out, &cases); err != nil {
		t.Fatalf("decode pwsh oracle JSON: %v\n%s", err, out)
	}
	return cases
}

func shellPowerShellInvocation(args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "pwsh")
	for _, arg := range args {
		parts = append(parts, quoteShellTokenForPowerShellTest(arg))
	}
	return strings.Join(parts, " ")
}

func quoteShellTokenForPowerShellTest(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\r\n\"'") {
		return arg
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(arg, `\`, `\\`), `"`, `\"`) + `"`
}

const powerShellCommandLineOracleScript = `
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$pwshPath = (Get-Process -Id $PID).Path
$assembly = [Reflection.Assembly]::LoadFrom((Join-Path $PSHOME 'Microsoft.PowerShell.ConsoleHost.dll'))
$parserType = $assembly.GetType('Microsoft.PowerShell.CommandLineParameterParser', $true)
$instanceFlags = [Reflection.BindingFlags] 'Instance,Public,NonPublic'
$staticFlags = [Reflection.BindingFlags] 'Static,Public,NonPublic'
$parseMethod = $parserType.GetMethod('Parse', $instanceFlags)
$initialCommandProperty = $parserType.GetProperty('InitialCommand', $instanceFlags)
$abortStartupProperty = $parserType.GetProperty('AbortStartup', $instanceFlags)

$tokens = [ordered] @{}
function Add-Token {
    param([string] $Token)

    if ([string]::IsNullOrWhiteSpace($Token)) {
        return
    }
    $normalized = $Token.ToLowerInvariant()
    if ($normalized -notmatch '^-+[a-z?][a-z0-9?]*$') {
        return
    }
    $tokens[$normalized] = $normalized
}

$validParametersField = $parserType.GetField('s_validParameters', $staticFlags)
foreach ($name in @($validParametersField.GetValue($null))) {
    $lower = $name.ToLowerInvariant()
    for ($i = 1; $i -le $lower.Length; $i++) {
        Add-Token ('-' + $lower.Substring(0, $i))
    }
}

$helpLines = & $pwshPath -NoLogo -NoProfile -NonInteractive --help | Where-Object { $_ -match '^-' }
foreach ($line in $helpLines) {
    foreach ($match in [regex]::Matches($line, '-[A-Za-z?][A-Za-z0-9?]*')) {
        Add-Token $match.Value
    }
}

foreach ($token in @($tokens.Keys)) {
    if ($token.StartsWith('-') -and -not $token.StartsWith('--') -and $token -ne '-?') {
        Add-Token ('-' + $token)
    }
}

$settingsFile = [IO.Path]::Combine(
    [IO.Path]::GetTempPath(),
    'tally-pwsh-parser-' + [Guid]::NewGuid().ToString('n') + '.json'
)
Set-Content -LiteralPath $settingsFile -Encoding UTF8 -Value '{}'

$marker = '` + powerShellCommandLineOracleMarker + `'
$valueCandidates = @(
    'Text',
    'Bypass',
    'Hidden',
    (Get-Location).Path,
    $settingsFile,
    'tallypipe',
    'Microsoft.PowerShell',
    '7.4'
)
$seen = @{}
$cases = [System.Collections.Generic.List[object]]::new()

function Invoke-Parser {
    param([string[]] $ArgsForParser)

    $parser = [Activator]::CreateInstance($parserType, $true)
    try {
        $parseMethod.Invoke($parser, [object[]] (, $ArgsForParser)) | Out-Null
    } catch {
        return $null
    }
    if ([bool] $abortStartupProperty.GetValue($parser)) {
        return $null
    }
    return [string] $initialCommandProperty.GetValue($parser)
}

function Add-Case {
    param(
        [string] $Token,
        [string] $Shape,
        [string[]] $ArgsForCase
    )

    $key = $ArgsForCase -join ([char] 31)
    if ($seen.ContainsKey($key)) {
        return
    }
    $seen[$key] = $true
    $cases.Add([pscustomobject] @{
        token = $Token
        shape = $Shape
        args = $ArgsForCase
    }) | Out-Null
}

foreach ($token in ($tokens.Keys | Sort-Object)) {
    $commandArgs = [string[]] @($token, $marker)
    if ((Invoke-Parser $commandArgs) -eq $marker) {
        Add-Case $token 'command' $commandArgs
    }

    $noValueArgs = [string[]] @($token, '-Command', $marker)
    if ((Invoke-Parser $noValueArgs) -eq $marker) {
        Add-Case $token 'before-command-no-value' $noValueArgs
    }

    foreach ($value in $valueCandidates) {
        $withValueArgs = [string[]] @($token, $value, '-Command', $marker)
        if ((Invoke-Parser $withValueArgs) -eq $marker) {
            Add-Case $token 'before-command-with-value' $withValueArgs
            break
        }
    }
}

Remove-Item -LiteralPath $settingsFile -ErrorAction SilentlyContinue
$cases.ToArray() | ConvertTo-Json -Compress -Depth 6 -AsArray
`
