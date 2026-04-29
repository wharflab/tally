package shellcheck

import (
	"strings"
	"testing"
	"time"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/testutil"
)

func TestShellcheckRunContextHasDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := shellcheckRunContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected shellcheck run context to have a deadline")
	}

	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > shellcheckRunTimeout {
		t.Fatalf("unexpected remaining timeout: %s", remaining)
	}
}

func TestGetShellFormScriptPrefersHeredocData(t *testing.T) {
	t.Parallel()

	run := parseRunCommand(t, `FROM alpine:3.20
RUN <<'EOF'
echo "$HOME"
EOF
`)
	if len(run.Files) == 0 || run.Files[0].Data == "" {
		t.Fatal("expected heredoc-backed RUN command")
	}

	got := getShellFormScript(run)
	if got != run.Files[0].Data {
		t.Fatalf("expected heredoc data %q, got %q", run.Files[0].Data, got)
	}
}

func TestGetShellFormScriptFallsBackToCmdLineWhenHeredocEmpty(t *testing.T) {
	t.Parallel()

	run := parseRunCommand(t, `FROM alpine:3.20
RUN echo $1 <<EOT
EOT
`)
	if len(run.Files) == 0 {
		t.Fatal("expected RUN command with heredoc metadata")
	}
	if run.Files[0].Data != "" {
		t.Fatalf("expected empty heredoc body, got %q", run.Files[0].Data)
	}

	want := strings.Join(run.CmdLine, " ")
	got := getShellFormScript(run)
	if got != want {
		t.Fatalf("expected fallback cmdline %q, got %q", want, got)
	}
}

func TestShellcheckDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		shellName string
		want      string
	}{
		{name: "default", shellName: "", want: "sh"},
		{name: "sh", shellName: "/bin/sh", want: "sh"},
		{name: "bash", shellName: "/bin/bash", want: "bash"},
		{name: "bats-maps-to-bash", shellName: "/usr/bin/bats", want: "bash"},
		{name: "windows-bash", shellName: `C:\Program Files\Git\bin\bash.exe`, want: "bash"},
		{name: "zsh-maps-to-bash", shellName: "/usr/bin/zsh", want: "bash"},
		{name: "dash", shellName: "/bin/dash", want: "dash"},
		{name: "ash", shellName: "/bin/ash", want: "busybox"},
		{name: "ksh", shellName: "/bin/ksh", want: "ksh"},
		{name: "mksh", shellName: "/bin/mksh", want: "ksh"},
		{name: "unknown-defaults-to-sh", shellName: "/opt/custom-shell", want: "sh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shellcheckDialect(tt.shellName)
			if got != tt.want {
				t.Fatalf("shellcheckDialect(%q) = %q, want %q", tt.shellName, got, tt.want)
			}
		})
	}
}

func TestIsAllowedShebangAcceptsBats(t *testing.T) {
	t.Parallel()

	for _, script := range []string{
		"#!/usr/bin/bats\n@test \"demo\" { run true; }\n",
		"#!/usr/bin/env bats\n@test \"demo\" { run true; }\n",
	} {
		if !isAllowedShebang(script) {
			t.Fatalf("expected bats shebang to be allowed: %q", script)
		}
	}
}

func TestCheckShellSnippetReportsViolationOnFallbackLocation(t *testing.T) {
	t.Parallel()

	r := NewRule()

	violations := r.checkShellSnippet(
		"Dockerfile",
		[]parser.Range{{Start: parser.Position{Line: 7, Character: 0}, End: parser.Position{Line: 7, Character: 12}}},
		"/bin/sh",
		nil,
		"echo $1",
	)
	if len(violations) == 0 {
		t.Fatal("expected at least one ShellCheck violation")
	}

	for _, v := range violations {
		if v.RuleCode != "shellcheck/SC2086" {
			continue
		}
		if v.Location.Start.Line != 7 || v.Location.End.Line != 7 {
			t.Fatalf("expected fallback location on line 7, got %+v", v.Location)
		}
		return
	}

	t.Fatalf("expected shellcheck/SC2086 in violations, got %+v", violations)
}

func TestCheckShellSnippetSkipsNonPOSIXShell(t *testing.T) {
	t.Parallel()

	r := &Rule{}
	violations := r.checkShellSnippet(
		"Dockerfile",
		[]parser.Range{{Start: parser.Position{Line: 2, Character: 0}}},
		"pwsh",
		nil,
		"echo $1",
	)
	if len(violations) != 0 {
		t.Fatalf("expected no violations for non-POSIX shell, got %+v", violations)
	}
}

func TestCheckShellSnippetSkipsNonParseableSnippet(t *testing.T) {
	t.Parallel()

	r := &Rule{}
	violations := r.checkShellSnippet(
		"Dockerfile",
		[]parser.Range{{Start: parser.Position{Line: 2, Character: 0}}},
		"/bin/sh",
		nil,
		"echo $1 <<EOT",
	)
	if len(violations) != 1 {
		t.Fatalf("expected parse-status violation for non-parseable snippet, got %+v", violations)
	}
	if violations[0].RuleCode != metaParseStatusRuleCode {
		t.Fatalf("expected rule %q, got %q", metaParseStatusRuleCode, violations[0].RuleCode)
	}
	if violations[0].Severity != rules.SeverityInfo {
		t.Fatalf("expected severity info, got %q", violations[0].Severity)
	}
	if strings.TrimSpace(violations[0].Detail) == "" {
		t.Fatalf("expected parse detail to be populated, got %+v", violations[0])
	}
}

func TestCheckShellSnippetParseErrorOwnsDiagnostics(t *testing.T) {
	t.Parallel()

	r := &Rule{}
	violations := r.checkShellSnippet(
		"Dockerfile",
		[]parser.Range{{Start: parser.Position{Line: 10, Character: 0}}},
		"/bin/sh",
		nil,
		"cat <<-EOF\nhello\n  EOF",
	)
	if len(violations) != 1 {
		t.Fatalf("expected one violation, got %+v", violations)
	}
	if violations[0].RuleCode != metaParseStatusRuleCode {
		t.Fatalf("expected rule %q, got %q", metaParseStatusRuleCode, violations[0].RuleCode)
	}
}

func TestCheckShellSnippetSkipsBlankSnippet(t *testing.T) {
	t.Parallel()

	r := &Rule{}
	violations := r.checkShellSnippet(
		"Dockerfile",
		[]parser.Range{{Start: parser.Position{Line: 2, Character: 0}}},
		"/bin/sh",
		nil,
		"   \n\t",
	)
	if len(violations) != 0 {
		t.Fatalf("expected no violations for blank snippet, got %+v", violations)
	}
}

func TestRule_DoesNotLintRunHeredocFilePayloadAsShellScript(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine
RUN <<EOF cat > /etc/app.conf
enable-rpc=true
rpc-listen-all=true
EOF
`)

	violations := NewRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no ShellCheck violations for config-file heredoc payload, got %+v", violations)
	}
}

func TestRule_DoesNotLintCmdStageAsPOSIXAfterSeparatePowerShellStage(t *testing.T) {
	t.Parallel()

	input := makeShellcheckLintInput(t, "Dockerfile", `FROM mcr.microsoft.com/powershell:nanoserver-ltsc2022 AS base
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
RUN Write-Host hi

FROM mcr.microsoft.com/windows/nanoserver:ltsc2022
RUN cmd /c icacls.exe C:\\BuildAgent\\* /grant:r DefaultAccount:(OI)(CI)F
RUN cmd /c icacls.exe C:\\BuildAgent\\* /grant:r Users:(OI)(CI)F
`)

	violations := NewRule().Check(input)
	for _, v := range violations {
		if v.Line() == 5 || v.Line() == 6 {
			t.Fatalf("expected no ShellCheck violations on cmd stage lines, got %+v", violations)
		}
	}
}

func TestRule_DoesNotLintExplicitPwshWrapperInCmdStage(t *testing.T) {
	t.Parallel()

	input := makeShellcheckLintInput(t, "Dockerfile", `FROM mcr.microsoft.com/windows/nanoserver:ltsc2022
RUN pwsh -NoLogo -NoProfile -Command " \
    $stopTime = (get-date).AddMinutes(15); \
    $ErrorActionPreference = 'Stop' ; \
    $ProgressPreference = 'SilentlyContinue' ; \
    while(!(Test-Path -Path $env:PSModuleAnalysisCachePath)) { \
        Write-Host \"'Waiting for $env:PSModuleAnalysisCachePath'\" ; \
        if((get-date) -gt $stopTime) { throw 'timeout expired'} \
        Start-Sleep -Seconds 6 ; \
    }"
`)

	violations := NewRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no ShellCheck violations for explicit pwsh wrapper, got %+v", violations)
	}
}

func TestRule_DoesNotLintExplicitWrappersWhenBaseImageOSIsUnknown(t *testing.T) {
	t.Parallel()

	input := makeShellcheckLintInput(t, "Dockerfile", `ARG base
FROM ${base}
RUN pwsh -NoLogo -NoProfile -Command "Write-Host hi"
RUN cmd /c icacls.exe C:\\BuildAgent\\* /grant:r Users:(OI)(CI)F
`)

	violations := NewRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected no ShellCheck violations for explicit non-POSIX wrappers, got %+v", violations)
	}
}

func makeShellcheckLintInput(tb testing.TB, file, content string) rules.LintInput {
	tb.Helper()

	result, err := dockerfile.Parse(strings.NewReader(content), nil)
	if err != nil {
		tb.Fatalf("failed to parse Dockerfile: %v", err)
	}

	sm := sourcemap.New(result.Source)
	dirResult := directive.Parse(sm, nil, nil)
	sem := semantic.NewBuilder(result, nil, file).
		WithShellDirectives(directive.ToSemanticShellDirectives(dirResult.ShellDirectives)).
		Build()

	return rules.LintInput{
		File:     file,
		AST:      result.AST,
		Stages:   result.Stages,
		MetaArgs: result.MetaArgs,
		Source:   result.Source,
		Semantic: sem,
	}
}

func parseRunCommand(t *testing.T, dockerfileContent string) *instructions.RunCommand {
	t.Helper()

	result, err := parser.Parse(strings.NewReader(dockerfileContent))
	if err != nil {
		t.Fatalf("parse Dockerfile: %v", err)
	}

	stages, _, err := instructions.Parse(result.AST, nil)
	if err != nil {
		t.Fatalf("parse instructions: %v", err)
	}
	for _, stage := range stages {
		for _, cmd := range stage.Commands {
			if run, ok := cmd.(*instructions.RunCommand); ok {
				return run
			}
		}
	}
	t.Fatal("no RUN command found")
	return nil
}
