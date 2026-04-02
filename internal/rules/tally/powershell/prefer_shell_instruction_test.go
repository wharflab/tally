package powershell

import (
	"context"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	tallyrules "github.com/wharflab/tally/internal/rules/tally"
	"github.com/wharflab/tally/internal/testutil"
)

const testPowerShellPrelude = "$ErrorActionPreference = 'Stop'; " +
	"$PSNativeCommandUseErrorActionPreference = $true; " +
	"$ProgressPreference = 'SilentlyContinue';"

var (
	testPwshShell                = testPowerShellShellLine("pwsh")
	testPwshNoProfileShell       = testPowerShellShellLine("pwsh", "-NoProfile")
	testPwshNoLogoNoProfileShell = testPowerShellShellLine("pwsh", "-NoLogo", "-NoProfile")
	testPowershellShell          = testPowerShellShellLine("powershell")
)

func testPowerShellShellLine(executable string, args ...string) string {
	parts := make([]string, 0, len(args)+3)
	parts = append(parts, `"`+executable+`"`)
	for _, arg := range args {
		parts = append(parts, `"`+arg+`"`)
	}
	parts = append(parts, `"-Command"`, `"`+testPowerShellPrelude+`"`)
	return "SHELL [" + strings.Join(parts, ",") + "]"
}

func TestPreferShellInstructionRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferShellInstructionRule().Metadata())
}

func TestPreferShellInstructionRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferShellInstructionRule(), []testutil.RuleTestCase{
		{
			Name: "linux repeated pwsh wrappers",
			Content: `FROM mcr.microsoft.com/powershell:ubuntu-22.04
RUN pwsh -Command Install-Module PSReadLine -Force
RUN pwsh -Command Invoke-WebRequest https://example.com/file.zip -OutFile /tmp/file.zip
`,
			WantViolations: 1,
			WantCodes:      []string{PreferShellInstructionRuleCode},
		},
		{
			Name: "windows repeated powershell wrappers",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command Invoke-WebRequest https://example.com/file.zip -OutFile C:\temp\file.zip
RUN @powershell -Command Start-Process notepad.exe
`,
			WantViolations: 1,
		},
		{
			Name: "windows bare powershell commands qualify",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell Add-Content C:\temp\proof.txt one
RUN powershell Add-Content C:\temp\proof.txt two
`,
			WantViolations: 1,
		},
		{
			Name: "single wrapper does not trigger",
			Content: `FROM alpine
RUN pwsh -Command Write-Host hi
`,
			WantViolations: 0,
		},
		{
			Name: "existing powershell shell does not trigger",
			Content: `FROM alpine
SHELL ["pwsh", "-Command"]
RUN Write-Host hi
RUN Write-Host bye
`,
			WantViolations: 0,
		},
		{
			Name: "exec form does not count",
			Content: `FROM alpine
RUN ["pwsh", "-Command", "Write-Host hi"]
RUN pwsh -Command Write-Host bye
`,
			WantViolations: 0,
		},
		{
			Name: "normal shell run breaks the cluster",
			Content: `FROM alpine
RUN pwsh -Command Write-Host hi
RUN echo still-sh
RUN pwsh -Command Write-Host bye
`,
			WantViolations: 0,
		},
	})
}

func TestPreferShellInstructionRule_Fix(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM alpine
RUN pwsh -NoProfile -Command "Write-Host hi"
ENV FOO=bar
RUN pwsh -NoProfile -Command Write-Host bye
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected suggested fix")
	}
	if fix.Safety != rules.FixSuggestion {
		t.Fatalf("fix safety = %v, want %v", fix.Safety, rules.FixSuggestion)
	}
	if len(fix.Edits) != 3 {
		t.Fatalf("fix edits = %d, want 3", len(fix.Edits))
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM alpine
` + testPwshNoProfileShell + `
RUN Write-Host hi
ENV FOO=bar
RUN Write-Host bye
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixPreservesLeadingCommentBlock(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
# Install chocolatey
RUN powershell -c "Write-Host hi"
RUN powershell -c "Write-Host bye"
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
` + testPowershellShell + `
# Install chocolatey
RUN Write-Host hi
RUN Write-Host bye
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixTranslatesCompatibleCmdRun(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -c "Write-Host hi"
RUN powershell -c "Write-Host bye"
RUN setx PATH '%PATH%;C:\Tools'
RUN py -m pip install -U pip
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
` + testPowershellShell + `
RUN Write-Host hi
RUN Write-Host bye
RUN setx path "$env:Path;C:\Tools"
RUN py -m pip install -U pip
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixSpansSafeInterveningWindowsCommands(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -NoProfile -ExecutionPolicy Bypass -Command "Write-Host bootstrap"
RUN powershell Add-Content C:\temp\proof.txt one && choco install git -y
RUN md C:\build
WORKDIR C:/build
COPY . C:/build
RUN powershell Add-Content C:\temp\proof.txt two
RUN xcopy C:\build\* C:\dest /s
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -NoProfile -ExecutionPolicy Bypass -Command "Write-Host bootstrap"
` + testPowershellShell + `
RUN Add-Content C:\temp\proof.txt one; \
    choco install git -y
RUN md C:\build
WORKDIR C:/build
COPY . C:/build
RUN Add-Content C:\temp\proof.txt two
RUN xcopy C:\build\* C:\dest /s
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixPreservesRunFlags(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/powershell:ubuntu-22.04
RUN --network=none pwsh -NoLogo -NoProfile -Command "Write-Host hi"
RUN --mount=type=cache,target=/tmp/cache pwsh -NoLogo -NoProfile -Command Write-Host bye
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/powershell:ubuntu-22.04
` + testPwshNoLogoNoProfileShell + `
RUN --network=none Write-Host hi
RUN --mount=type=cache,target=/tmp/cache Write-Host bye
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_IgnoresMixedShellPrefixes(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN pwsh -NoProfile -Command Write-Host hi
RUN pwsh -Command Write-Host bye
`)

	violations := rule.Check(input)
	if len(violations) != 0 {
		t.Fatalf("got %d violations, want 0", len(violations))
	}
}

func TestPreferShellInstructionRule_FixRestoresShellBeforeLaterPOSIXRun(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM alpine
RUN pwsh -Command Write-Host hi
RUN pwsh -Command Write-Host bye
RUN apk add --no-cache curl
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix when later POSIX RUN can be preserved with shell restore")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM alpine
` + testPwshShell + `
RUN Write-Host hi
RUN Write-Host bye
SHELL ["/bin/sh","-c"]
RUN apk add --no-cache curl
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixRestoresShellBeforeRuntimeInstruction(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM alpine
RUN pwsh -Command Write-Host hi
CMD echo hi
RUN pwsh -Command Write-Host bye
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix when CMD can be shielded by shell restore")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM alpine
` + testPwshShell + `
RUN Write-Host hi
SHELL ["/bin/sh","-c"]
CMD echo hi
RUN pwsh -Command Write-Host bye
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixUnwrapsShellFormCmdUnderPowerShell(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command Write-Host hi
RUN powershell -Command Write-Host bye
CMD powershell -Command Write-Host ready
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
` + testPowershellShell + `
RUN Write-Host hi
RUN Write-Host bye
CMD Write-Host ready
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixUnwrapsShellFormEntrypointUnderPowerShell(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command Write-Host hi
RUN powershell -Command Write-Host bye
ENTRYPOINT powershell -Command .\Startup.ps1
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
` + testPowershellShell + `
RUN Write-Host hi
RUN Write-Host bye
ENTRYPOINT .\Startup.ps1
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixAllowsCmdSlashCWrapper(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -c "Write-Host hi"
RUN powershell -c "Write-Host bye"
RUN cmd /c 'echo one && echo two'
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}
}

func TestPreferShellInstructionRule_FixAllowsCompatibleCmdBuiltins(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -c "Write-Host hi"
RUN powershell -c "Write-Host bye"
RUN md c:\build
RUN cd c:\build
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
` + testPowershellShell + `
RUN Write-Host hi
RUN Write-Host bye
RUN md c:\build
RUN cd c:\build
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixKeepsExternalCommandsUnderPowerShell(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -c "Write-Host hi"
RUN powershell -c "Write-Host bye"
RUN xcopy c:\build\TicketDesk.Web.Client\* c:\inetpub\wwwroot /s
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
` + testPowershellShell + `
RUN Write-Host hi
RUN Write-Host bye
RUN xcopy c:\build\TicketDesk.Web.Client\* c:\inetpub\wwwroot /s
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixBarePowerShellCommands(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell Write-Host hi
RUN powershell Write-Host bye
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
` + testPowershellShell + `
RUN Write-Host hi
RUN Write-Host bye
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixCmdChainAfterBarePowerShell(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell add-windowsfeature web-asp-net45 \
    && choco install microsoft-build-tools -y --allow-empty-checksums -version 14.0.23107.10 \
    && choco install dotnet4.6-targetpack --allow-empty-checksums -y
RUN powershell Write-Host done
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM mcr.microsoft.com/windows/servercore:ltsc2022
` + testPowershellShell + `
RUN add-windowsfeature web-asp-net45; \
    choco install microsoft-build-tools -y --allow-empty-checksums -version 14.0.23107.10; \
    choco install dotnet4.6-targetpack --allow-empty-checksums -y
RUN Write-Host done
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixCmdChainAfterBarePowerShellHonorsEscapeDirective(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	const content = "# escape=`\n" +
		"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
		"RUN powershell add-windowsfeature web-asp-net45 `\n" +
		"    && choco install microsoft-build-tools -y --allow-empty-checksums -version 14.0.23107.10\n" +
		"RUN powershell Write-Host done\n"

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatal("expected suggested fix")
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := "# escape=`\n" +
		"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
		testPowershellShell + "\n" +
		"RUN add-windowsfeature web-asp-net45; `\n" +
		"    choco install microsoft-build-tools -y --allow-empty-checksums -version 14.0.23107.10\n" +
		"RUN Write-Host done\n"
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_FixCollaboratesWithConsistentIndentation(t *testing.T) {
	t.Parallel()

	powerShellRule := NewPreferShellInstructionRule()
	indentRule := tallyrules.NewConsistentIndentationRule()
	const content = `FROM alpine AS base
	RUN echo base

FROM mcr.microsoft.com/windows/servercore:ltsc2022
	SHELL ["cmd","/S","/C"]
	RUN powershell add-windowsfeature web-asp-net45 \
	    && choco install microsoft-build-tools -y --allow-empty-checksums -version 14.0.23107.10
	RUN powershell Write-Host done
COPY . C:/app
`

	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := make([]rules.Violation, 0, 2)
	violations = append(violations, powerShellRule.Check(input)...)
	violations = append(violations, indentRule.Check(input)...)
	if len(violations) != 2 {
		t.Fatalf("got %d violations, want 2", len(violations))
	}

	sources := map[string][]byte{"Dockerfile": []byte(content)}
	fixer := &fixpkg.Fixer{SafetyThreshold: rules.FixSuggestion}
	result, err := fixer.Apply(context.Background(), violations, sources)
	if err != nil {
		t.Fatalf("apply fixes: %v", err)
	}

	got := string(result.Changes["Dockerfile"].ModifiedContent)
	want := `FROM alpine AS base
	RUN echo base

FROM mcr.microsoft.com/windows/servercore:ltsc2022
	SHELL ["cmd","/S","/C"]
	` + testPowershellShell + `
	RUN add-windowsfeature web-asp-net45; \
	    choco install microsoft-build-tools -y --allow-empty-checksums -version 14.0.23107.10
	RUN Write-Host done
	COPY . C:/app
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}
