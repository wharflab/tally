package facts

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

func TestFileFacts_BuildsRunFactsWithEnvShellAndCommands(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, "Dockerfile", `# hadolint shell=bash
FROM alpine:3.20
ENV DEBIAN_FRONTEND=noninteractive npm_config_cache=.npm
WORKDIR /app
RUN env PIP_INDEX_URL=https://example.com/simple pip install flask && npm install express
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if stage.InitialShell.Variant != shell.VariantBash {
		t.Fatalf("expected initial shell variant %v, got %v", shell.VariantBash, stage.InitialShell.Variant)
	}
	if len(stage.Runs) != 1 {
		t.Fatalf("expected 1 RUN fact, got %d", len(stage.Runs))
	}

	run := stage.Runs[0]
	if run.Workdir != "/app" {
		t.Fatalf("expected workdir /app, got %q", run.Workdir)
	}
	if !run.Env.AptNonInteractive {
		t.Fatal("expected DEBIAN_FRONTEND=noninteractive to be reflected in env facts")
	}
	if got := run.CachePathOverrides["npm"]; got != "/app/.npm" {
		t.Fatalf("expected npm cache override /app/.npm, got %q", got)
	}
	if len(run.CommandInfos) != 3 {
		t.Fatalf("expected 3 command facts (env, pip, npm), got %d", len(run.CommandInfos))
	}
	if run.CommandInfos[0].Name != "env" || run.CommandInfos[1].Name != "pip" || run.CommandInfos[2].Name != "npm" {
		t.Fatalf("unexpected command sequence: %#v", run.CommandInfos)
	}
	if len(run.InstallCommands) != 2 {
		t.Fatalf("expected 2 install commands, got %d", len(run.InstallCommands))
	}
}

func TestResolveWorkdirAndUnquote(t *testing.T) {
	t.Parallel()

	if got := ResolveWorkdir("/app", "tmp/cache"); got != "/app/tmp/cache" {
		t.Fatalf("ResolveWorkdir() relative = %q, want %q", got, "/app/tmp/cache")
	}
	if got := ResolveWorkdir("/app", "/var/cache"); got != "/var/cache" {
		t.Fatalf("ResolveWorkdir() absolute = %q, want %q", got, "/var/cache")
	}
	if got := Unquote(`"quoted"`); got != "quoted" {
		t.Fatalf("Unquote() double-quoted = %q, want %q", got, "quoted")
	}
	if got := Unquote("'single'"); got != "single" {
		t.Fatalf("Unquote() single-quoted = %q, want %q", got, "single")
	}
	if got := Unquote("bare"); got != "bare" {
		t.Fatalf("Unquote() bare = %q, want %q", got, "bare")
	}
}

func TestShellDirectivesFromDirective(t *testing.T) {
	t.Parallel()

	got := ShellDirectivesFromDirective([]directive.ShellDirective{
		{Line: 3, Shell: "bash"},
		{Line: 7, Shell: "powershell"},
	})

	if len(got) != 2 {
		t.Fatalf("expected 2 shell directives, got %d", len(got))
	}
	if got[0] != (ShellDirective{Line: 3, Shell: "bash"}) {
		t.Fatalf("unexpected first shell directive: %#v", got[0])
	}
	if got[1] != (ShellDirective{Line: 7, Shell: "powershell"}) {
		t.Fatalf("unexpected second shell directive: %#v", got[1])
	}
	if ShellDirectivesFromDirective(nil) != nil {
		t.Fatal("expected nil result for nil input")
	}
}

func TestFileFacts_PowerShellErrorModeIsTrackedPerRun(t *testing.T) {
	t.Parallel()

	fileFacts := makeFileFacts(t, "Dockerfile", `FROM mcr.microsoft.com/powershell:nanoserver-ltsc2022
SHELL ["powershell","-Command","Write-Host hi"]
RUN npm install left-pad
SHELL ["powershell","-Command","$ErrorActionPreference = 'Stop'; Write-Host hi"]
RUN npm install lodash
`)

	stage := fileFacts.Stage(0)
	if stage == nil {
		t.Fatal("expected stage facts")
	}
	if stage.BaseImageOS != semantic.BaseImageOSWindows {
		t.Fatalf("expected windows base image, got %v", stage.BaseImageOS)
	}
	if len(stage.Runs) != 2 {
		t.Fatalf("expected 2 RUN facts, got %d", len(stage.Runs))
	}
	if !stage.Runs[0].Shell.IsPowerShell || !stage.Runs[0].Shell.PowerShellMayMaskErr {
		t.Fatal("expected first RUN to inherit masking PowerShell shell facts")
	}
	if !stage.Runs[1].Shell.IsPowerShell || stage.Runs[1].Shell.PowerShellMayMaskErr {
		t.Fatal("expected second RUN to inherit PowerShell shell facts with stop behavior")
	}
}

func makeFileFacts(t *testing.T, file, content string) *FileFacts {
	t.Helper()

	parseResult, err := dockerfile.Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("parse dockerfile: %v", err)
	}

	sm := sourcemap.New(parseResult.Source)
	spanIndex := directive.NewInstructionSpanIndexFromAST(parseResult.AST, sm)
	directiveResult := directive.Parse(sm, nil, spanIndex)
	sem := semantic.NewBuilder(parseResult, nil, file).
		WithShellDirectives(directiveResult.ShellDirectives).
		Build()

	return NewFileFacts(file, parseResult, sem, toShellDirectives(directiveResult.ShellDirectives))
}

func toShellDirectives(directives []directive.ShellDirective) []ShellDirective {
	if len(directives) == 0 {
		return nil
	}

	out := make([]ShellDirective, 0, len(directives))
	for _, directive := range directives {
		out = append(out, ShellDirective{
			Line:  directive.Line,
			Shell: directive.Shell,
		})
	}
	return out
}
