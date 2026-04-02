package powershell

import (
	"context"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	fixpkg "github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

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
SHELL ["pwsh","-NoProfile","-Command","$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
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
SHELL ["powershell","-Command","$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
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
SHELL ["powershell","-Command","$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
RUN Write-Host hi
RUN Write-Host bye
RUN setx path "$env:Path;C:\Tools"
RUN py -m pip install -U pip
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
SHELL ["pwsh","-NoLogo","-NoProfile","-Command","$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
RUN --network=none Write-Host hi
RUN --mount=type=cache,target=/tmp/cache Write-Host bye
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}

func TestPreferShellInstructionRule_NoFixForMixedShellPrefixes(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN pwsh -NoProfile -Command Write-Host hi
RUN pwsh -Command Write-Host bye
`)

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Fatal("expected no suggested fix")
	}
}

func TestPreferShellInstructionRule_NoFixWhenLaterPOSIXRunWouldBeCaptured(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN pwsh -Command Write-Host hi
RUN pwsh -Command Write-Host bye
RUN apk add --no-cache curl
`)

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Fatal("expected no suggested fix when later POSIX RUN would change shell")
	}
}

func TestPreferShellInstructionRule_NoFixAcrossRuntimeShellSensitiveInstructions(t *testing.T) {
	t.Parallel()

	rule := NewPreferShellInstructionRule()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN pwsh -Command Write-Host hi
CMD echo hi
RUN pwsh -Command Write-Host bye
`)

	violations := rule.Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix != nil {
		t.Fatal("expected no suggested fix when CMD would be captured by inserted SHELL")
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
SHELL ["powershell","-Command","$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
RUN Write-Host hi
RUN Write-Host bye
`
	if got != want {
		t.Fatalf("fixed content =\n%s\nwant:\n%s", got, want)
	}
}
