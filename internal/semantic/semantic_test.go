package semantic

import (
	"slices"
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/dockerfile"
)

// parseDockerfile is a test helper that parses a Dockerfile string.
func parseDockerfile(t *testing.T, content string) *dockerfile.ParseResult {
	t.Helper()
	pr, err := dockerfile.Parse(strings.NewReader(content), nil)
	if err != nil {
		t.Fatalf("failed to parse Dockerfile: %v", err)
	}
	return pr
}

func TestEmptyDockerfile(t *testing.T) {
	t.Parallel()
	// Empty Dockerfiles return parse error from BuildKit
	// Test that we handle nil parse result gracefully
	model := NewModel(nil, nil, "Dockerfile")

	if model.StageCount() != 0 {
		t.Errorf("expected 0 stages, got %d", model.StageCount())
	}
	if len(model.ConstructionIssues()) != 0 {
		t.Errorf("expected 0 violations, got %d", len(model.ConstructionIssues()))
	}
}

func TestSingleStage(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN echo "hello"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	if model.StageCount() != 1 {
		t.Fatalf("expected 1 stage, got %d", model.StageCount())
	}

	stage := model.Stage(0)
	if stage == nil {
		t.Fatal("stage 0 should not be nil")
	}
	if stage.BaseName != "alpine:3.18" {
		t.Errorf("expected base name 'alpine:3.18', got %q", stage.BaseName)
	}

	info := model.StageInfo(0)
	if info == nil {
		t.Fatal("stage info 0 should not be nil")
	}
	if !info.IsLastStage {
		t.Error("single stage should be marked as last stage")
	}
	if info.BaseImage.Raw != "alpine:3.18" {
		t.Errorf("expected base image 'alpine:3.18', got %q", info.BaseImage.Raw)
	}
	if info.BaseImage.IsStageRef {
		t.Error("base image should not be a stage ref")
	}
}

func TestMultipleStages(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.18
COPY --from=builder /app /app
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	if model.StageCount() != 2 {
		t.Fatalf("expected 2 stages, got %d", model.StageCount())
	}

	// First stage
	info0 := model.StageInfo(0)
	if info0.IsLastStage {
		t.Error("first stage should not be marked as last stage")
	}
	if info0.Stage.Name != "builder" {
		t.Errorf("expected stage name 'builder', got %q", info0.Stage.Name)
	}

	// Second stage
	info1 := model.StageInfo(1)
	if !info1.IsLastStage {
		t.Error("second stage should be marked as last stage")
	}

	// Stage lookup by name
	builderStage := model.StageByName("builder")
	if builderStage == nil {
		t.Fatal("should find stage by name 'builder'")
	}
	if builderStage.Name != "builder" {
		t.Errorf("expected stage name 'builder', got %q", builderStage.Name)
	}

	// Stage name is case-insensitive
	if model.StageByName("BUILDER") == nil {
		t.Error("stage lookup should be case-insensitive")
	}
}

func TestNamedAndUnnamedStages(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18 AS first
RUN echo "first"

FROM ubuntu:22.04
RUN echo "second"

FROM debian:12 AS third
RUN echo "third"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	if model.StageCount() != 3 {
		t.Fatalf("expected 3 stages, got %d", model.StageCount())
	}

	// Named stages should be findable
	if model.StageByName("first") == nil {
		t.Error("should find stage 'first'")
	}
	if model.StageByName("third") == nil {
		t.Error("should find stage 'third'")
	}

	// Unnamed stage (second) should not be findable by empty name
	if model.StageByName("") != nil {
		t.Error("should not find stage by empty name")
	}

	// Second stage has empty name
	info1 := model.StageInfo(1)
	if info1.Stage.Name != "" {
		t.Errorf("expected empty stage name, got %q", info1.Stage.Name)
	}
}

func TestDL3012MultipleHealthcheck(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
HEALTHCHECK CMD echo ok
HEALTHCHECK NONE
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	violations := model.ConstructionIssues()
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Code != "hadolint/DL3012" {
		t.Errorf("expected hadolint/DL3012, got %q", violations[0].Code)
	}
	if violations[0].Location.Start.Line != 3 {
		t.Errorf("expected violation on line 3, got %d", violations[0].Location.Start.Line)
	}
}

func TestDL3012ResetsPerStage(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
HEALTHCHECK NONE

FROM alpine:3.18
HEALTHCHECK NONE
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	violations := model.ConstructionIssues()
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations, got %d", len(violations))
	}
}

func TestDL3023CopyFromOwnAlias(t *testing.T) {
	t.Parallel()
	content := `FROM node:20 AS foo
COPY --from=foo bar .
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	violations := model.ConstructionIssues()
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
	// DL3023 fires first (checked before processCopyFrom), then DL3022
	if violations[0].Code != "hadolint/DL3023" {
		t.Errorf("expected hadolint/DL3023, got %q", violations[0].Code)
	}
	// DL3022: COPY --from references undefined alias (self-reference is not "previously defined")
	if violations[1].Code != "hadolint/DL3022" {
		t.Errorf("expected hadolint/DL3022, got %q", violations[1].Code)
	}
	for _, v := range violations {
		if v.Location.Start.Line != 2 {
			t.Errorf("expected %s violation on line 2, got %d", v.Code, v.Location.Start.Line)
		}
	}
}

func TestDL3043OnbuildForbiddenInstructions(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
ONBUILD FROM debian:buster
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	violations := model.ConstructionIssues()
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Code != "hadolint/DL3043" {
		t.Errorf("expected hadolint/DL3043, got %q", violations[0].Code)
	}
	if violations[0].Location.Start.Line != 2 {
		t.Errorf("expected violation on line 2, got %d", violations[0].Location.Start.Line)
	}
}

func TestDL3061InvalidInstructionOrder(t *testing.T) {
	t.Parallel()
	content := `ARG FOO=bar
RUN echo "hello"
ARG BAR=baz
FROM alpine:3.18
RUN echo "ok"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	violations := model.ConstructionIssues()
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Code != "hadolint/DL3061" {
		t.Errorf("expected hadolint/DL3061, got %q", violations[0].Code)
	}
	if violations[0].Location.Start.Line != 2 {
		t.Errorf("expected violation on line 2, got %d", violations[0].Location.Start.Line)
	}
}

func TestVariableResolutionBasic(t *testing.T) {
	t.Parallel()
	content := `ARG VERSION=1.0
FROM alpine:3.18
ARG VERSION
ARG OTHER=default
RUN echo $VERSION $OTHER
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	// Without build args, VERSION has no value in stage (only declared, no default in stage)
	val, found := model.ResolveVariable(0, "VERSION")
	if !found {
		t.Error("VERSION should be found")
	}
	// Global ARG has default, stage ARG inherits it
	if val != "1.0" {
		t.Errorf("expected VERSION='1.0', got %q", val)
	}

	// OTHER has default
	val, found = model.ResolveVariable(0, "OTHER")
	if !found {
		t.Error("OTHER should be found")
	}
	if val != "default" {
		t.Errorf("expected OTHER='default', got %q", val)
	}
}

func TestVariableResolutionBuildArgOverride(t *testing.T) {
	t.Parallel()
	content := `ARG VERSION=1.0
FROM alpine:3.18
ARG VERSION
RUN echo $VERSION
`
	pr := parseDockerfile(t, content)
	buildArgs := map[string]string{"VERSION": "2.0"}
	model := NewModel(pr, buildArgs, "Dockerfile")

	// Build arg should override
	val, found := model.ResolveVariable(0, "VERSION")
	if !found {
		t.Error("VERSION should be found")
	}
	if val != "2.0" {
		t.Errorf("expected VERSION='2.0' (build arg override), got %q", val)
	}
}

func TestVariableResolutionENVOverridesARG(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
ARG MYVAR=arg_value
ENV MYVAR=env_value
RUN echo $MYVAR
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	// ENV should override ARG
	val, found := model.ResolveVariable(0, "MYVAR")
	if !found {
		t.Error("MYVAR should be found")
	}
	if val != "env_value" {
		t.Errorf("expected MYVAR='env_value' (ENV overrides ARG), got %q", val)
	}
}

func TestVariableResolutionENVOverridesBuildArg(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
ARG MYVAR=arg_value
ENV MYVAR=env_value
RUN echo $MYVAR
`
	pr := parseDockerfile(t, content)
	buildArgs := map[string]string{"MYVAR": "build_arg_value"}
	model := NewModel(pr, buildArgs, "Dockerfile")

	// Build arg for declared ARG should take precedence even over ENV
	// Actually, build args only affect ARG instructions, not ENV
	// So ENV still wins here
	val, found := model.ResolveVariable(0, "MYVAR")
	if !found {
		t.Error("MYVAR should be found")
	}
	// Correct precedence: ENV > ARG (build arg affects ARG value)
	// When we look up MYVAR, we find ENV first
	if val != "env_value" {
		t.Errorf("expected MYVAR='env_value' (ENV found first), got %q", val)
	}
}

func TestCopyFromNamedStage(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.18
COPY --from=builder /app /app
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(1)
	if len(info.CopyFromRefs) != 1 {
		t.Fatalf("expected 1 COPY --from ref, got %d", len(info.CopyFromRefs))
	}

	ref := info.CopyFromRefs[0]
	if ref.From != "builder" {
		t.Errorf("expected From='builder', got %q", ref.From)
	}
	if !ref.IsStageRef {
		t.Error("should be a stage ref")
	}
	if ref.StageIndex != 0 {
		t.Errorf("expected StageIndex=0, got %d", ref.StageIndex)
	}
}

func TestCopyFromNumericIndex(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21
RUN go build -o /app

FROM alpine:3.18
COPY --from=0 /app /app
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(1)
	if len(info.CopyFromRefs) != 1 {
		t.Fatalf("expected 1 COPY --from ref, got %d", len(info.CopyFromRefs))
	}

	ref := info.CopyFromRefs[0]
	if ref.From != "0" {
		t.Errorf("expected From='0', got %q", ref.From)
	}
	if !ref.IsStageRef {
		t.Error("should be a stage ref")
	}
	if ref.StageIndex != 0 {
		t.Errorf("expected StageIndex=0, got %d", ref.StageIndex)
	}
}

func TestCopyFromExternalImage(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
COPY --from=nginx:latest /etc/nginx/nginx.conf /nginx.conf
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)
	if len(info.CopyFromRefs) != 1 {
		t.Fatalf("expected 1 COPY --from ref, got %d", len(info.CopyFromRefs))
	}

	ref := info.CopyFromRefs[0]
	if ref.From != "nginx:latest" {
		t.Errorf("expected From='nginx:latest', got %q", ref.From)
	}
	if ref.IsStageRef {
		t.Error("should not be a stage ref (external image)")
	}
	if ref.StageIndex != -1 {
		t.Errorf("expected StageIndex=-1 for external, got %d", ref.StageIndex)
	}

	// Check graph has external ref
	externalRefs := model.Graph().ExternalRefs(0)
	if len(externalRefs) != 1 || externalRefs[0] != "nginx:latest" {
		t.Errorf("expected external ref 'nginx:latest', got %v", externalRefs)
	}
}

func TestSHELLInheritance(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN echo "default shell"
SHELL ["/bin/bash", "-c"]
RUN echo "bash shell"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)

	// After SHELL instruction, shell should be updated
	expectedShell := []string{"/bin/bash", "-c"}
	if len(info.ShellSetting.Shell) != len(expectedShell) {
		t.Fatalf("expected shell %v, got %v", expectedShell, info.ShellSetting.Shell)
	}
	for i, s := range expectedShell {
		if info.ShellSetting.Shell[i] != s {
			t.Errorf("expected shell[%d]=%q, got %q", i, s, info.ShellSetting.Shell[i])
		}
	}
}

func TestDefaultShell(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
RUN echo "hello"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)

	expectedShell := []string{"/bin/sh", "-c"}
	if len(info.ShellSetting.Shell) != len(expectedShell) {
		t.Fatalf("expected default shell %v, got %v", expectedShell, info.ShellSetting.Shell)
	}
	for i, s := range expectedShell {
		if info.ShellSetting.Shell[i] != s {
			t.Errorf("expected shell[%d]=%q, got %q", i, s, info.ShellSetting.Shell[i])
		}
	}
}

func TestStageGraphDependencies(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM nginx:alpine AS webserver
COPY --from=builder /app /app

FROM alpine:3.18
COPY --from=webserver /app /final
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	graph := model.Graph()

	// Stage 1 (webserver) depends on stage 0 (builder)
	if !graph.DependsOn(1, 0) {
		t.Error("stage 1 should depend on stage 0")
	}

	// Stage 2 depends on stage 1
	if !graph.DependsOn(2, 1) {
		t.Error("stage 2 should depend on stage 1")
	}

	// Stage 2 transitively depends on stage 0
	if !graph.DependsOn(2, 0) {
		t.Error("stage 2 should transitively depend on stage 0")
	}

	// Stage 0 does not depend on anything
	if graph.DependsOn(0, 1) || graph.DependsOn(0, 2) {
		t.Error("stage 0 should not depend on later stages")
	}
}

func TestUnreachableStages(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM golang:1.21 AS unused
RUN echo "this is never used"

FROM alpine:3.18
COPY --from=builder /app /app
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	graph := model.Graph()
	unreachable := graph.UnreachableStages()

	// Stage 1 (unused) should be unreachable from stage 2
	if len(unreachable) != 1 {
		t.Fatalf("expected 1 unreachable stage, got %d: %v", len(unreachable), unreachable)
	}
	if unreachable[0] != 1 {
		t.Errorf("expected unreachable stage 1, got %d", unreachable[0])
	}
}

func TestIsReachable(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.18
COPY --from=builder /app /app
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	graph := model.Graph()

	// Final stage is always reachable from itself
	if !graph.IsReachable(1, 1) {
		t.Error("final stage should be reachable from itself")
	}

	// Builder stage is reachable from final stage
	if !graph.IsReachable(0, 1) {
		t.Error("builder stage should be reachable from final stage")
	}
}

func TestBaseImageFromStage(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM builder AS extended
RUN echo "extending builder"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(1)

	if !info.BaseImage.IsStageRef {
		t.Error("base image should be a stage ref")
	}
	if info.BaseImage.StageIndex != 0 {
		t.Errorf("expected StageIndex=0, got %d", info.BaseImage.StageIndex)
	}
	if info.BaseImage.Raw != "builder" {
		t.Errorf("expected Raw='builder', got %q", info.BaseImage.Raw)
	}
}

func TestBaseImageStageReachability(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18 AS base
RUN echo "base"

FROM base
RUN echo "final"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	if !model.Graph().IsReachable(0, 1) {
		t.Error("base stage should be reachable when used as FROM")
	}

	// Base stage used via FROM should not be unreachable
	unreachable := model.Graph().UnreachableStages()
	for _, idx := range unreachable {
		if idx == 0 {
			t.Error("base stage should not be unreachable when used as FROM")
		}
	}
}

func TestOnbuildCopyFrom(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.18
ONBUILD COPY --from=builder /app /app
ONBUILD RUN echo "not a copy"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	// Stage 1 should have ONBUILD COPY --from reference
	info := model.StageInfo(1)
	if len(info.OnbuildCopyFromRefs) != 1 {
		t.Fatalf("expected 1 ONBUILD COPY --from ref, got %d", len(info.OnbuildCopyFromRefs))
	}

	ref := info.OnbuildCopyFromRefs[0]
	if ref.From != "builder" {
		t.Errorf("expected From='builder', got %q", ref.From)
	}
	if !ref.IsStageRef {
		t.Error("should be a stage ref")
	}
	if ref.StageIndex != 0 {
		t.Errorf("expected StageIndex=0, got %d", ref.StageIndex)
	}

	// Regular CopyFromRefs should be empty
	if len(info.CopyFromRefs) != 0 {
		t.Errorf("expected 0 regular COPY --from refs, got %d", len(info.CopyFromRefs))
	}

	// ONBUILD COPY --from should NOT make the builder stage reachable in the current build.
	// ONBUILD instructions only execute when the image is used as a base for another build.
	if model.Graph().IsReachable(0, 1) {
		t.Error("builder stage should NOT be reachable via ONBUILD COPY --from (ONBUILD executes in downstream builds)")
	}

	// Builder stage should be unreachable since it's only referenced by ONBUILD
	unreachable := model.Graph().UnreachableStages()
	hasBuilder := slices.Contains(unreachable, 0)
	if !hasBuilder {
		t.Error("builder stage should be unreachable (only referenced via ONBUILD)")
	}
}

func TestOnbuildCopyFromNumeric(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21
RUN go build -o /app

FROM alpine:3.18
ONBUILD COPY --from=0 /app /app
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(1)
	if len(info.OnbuildCopyFromRefs) != 1 {
		t.Fatalf("expected 1 ONBUILD COPY --from ref, got %d", len(info.OnbuildCopyFromRefs))
	}

	ref := info.OnbuildCopyFromRefs[0]
	if ref.From != "0" {
		t.Errorf("expected From='0', got %q", ref.From)
	}
	if !ref.IsStageRef {
		t.Error("should be a stage ref")
	}
	if ref.StageIndex != 0 {
		t.Errorf("expected StageIndex=0, got %d", ref.StageIndex)
	}
}

func TestOnbuildCopyFromExternal(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18
ONBUILD COPY --from=nginx:latest /etc/nginx/nginx.conf /nginx.conf
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)
	if len(info.OnbuildCopyFromRefs) != 1 {
		t.Fatalf("expected 1 ONBUILD COPY --from ref, got %d", len(info.OnbuildCopyFromRefs))
	}

	ref := info.OnbuildCopyFromRefs[0]
	if ref.From != "nginx:latest" {
		t.Errorf("expected From='nginx:latest', got %q", ref.From)
	}
	if ref.IsStageRef {
		t.Error("should not be a stage ref (external image)")
	}
	if ref.StageIndex != -1 {
		t.Errorf("expected StageIndex=-1 for external, got %d", ref.StageIndex)
	}
}

func TestNilParseResult(t *testing.T) {
	t.Parallel()
	model := NewModel(nil, nil, "Dockerfile")

	if model.StageCount() != 0 {
		t.Errorf("expected 0 stages for nil parse result, got %d", model.StageCount())
	}
	if model.Stage(0) != nil {
		t.Error("Stage(0) should return nil for empty model")
	}
	if model.StageInfo(0) != nil {
		t.Error("StageInfo(0) should return nil for empty model")
	}
}

func TestStageIndexByName(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.18 AS first
FROM ubuntu:22.04 AS second
FROM debian:12 AS third
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	tests := []struct {
		name      string
		wantIdx   int
		wantFound bool
	}{
		{"first", 0, true},
		{"FIRST", 0, true}, // case insensitive
		{"second", 1, true},
		{"third", 2, true},
		{"nonexistent", -1, false},
		{"", -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			idx, found := model.StageIndexByName(tt.name)
			if found != tt.wantFound {
				t.Errorf("StageIndexByName(%q) found = %v, want %v", tt.name, found, tt.wantFound)
			}
			if found && idx != tt.wantIdx {
				t.Errorf("StageIndexByName(%q) idx = %d, want %d", tt.name, idx, tt.wantIdx)
			}
		})
	}
}

func TestMetaArgs(t *testing.T) {
	t.Parallel()
	content := `ARG BASE=alpine
ARG VERSION=3.18
FROM ${BASE}:${VERSION}
RUN echo "hello"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	metaArgs := model.MetaArgs()
	if len(metaArgs) != 2 {
		t.Fatalf("expected 2 meta args, got %d", len(metaArgs))
	}

	// Check first meta arg
	if len(metaArgs[0].Args) != 1 || metaArgs[0].Args[0].Key != "BASE" {
		t.Errorf("expected first meta arg to be BASE, got %+v", metaArgs[0])
	}

	// Check second meta arg
	if len(metaArgs[1].Args) != 1 || metaArgs[1].Args[0].Key != "VERSION" {
		t.Errorf("expected second meta arg to be VERSION, got %+v", metaArgs[1])
	}
}

func TestGraphDirectDependencies(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM nginx:alpine AS webserver
COPY --from=builder /app /app

FROM alpine:3.18
COPY --from=builder /app /builder-app
COPY --from=webserver /app /webserver-app
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	graph := model.Graph()

	// Stage 2 directly depends on both 0 and 1
	deps := graph.DirectDependencies(2)
	if len(deps) != 2 {
		t.Fatalf("expected 2 direct dependencies, got %d", len(deps))
	}
	// Verify actual values (order may vary, so check both are present)
	hasDep0, hasDep1 := false, false
	for _, d := range deps {
		if d == 0 {
			hasDep0 = true
		}
		if d == 1 {
			hasDep1 = true
		}
	}
	if !hasDep0 || !hasDep1 {
		t.Errorf("expected dependencies on stages 0 and 1, got %v", deps)
	}

	// Stage 1 only directly depends on 0
	deps = graph.DirectDependencies(1)
	if len(deps) != 1 || deps[0] != 0 {
		t.Errorf("expected stage 1 to only depend on stage 0, got %v", deps)
	}
}

func TestGraphDirectDependents(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.18 AS stage1
COPY --from=builder /app /app

FROM alpine:3.18 AS stage2
COPY --from=builder /app /app
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	graph := model.Graph()

	// Stage 0 (builder) is depended on by stages 1 and 2
	dependents := graph.DirectDependents(0)
	if len(dependents) != 2 {
		t.Fatalf("expected 2 dependents, got %d: %v", len(dependents), dependents)
	}
	// Verify actual values (order may vary, so check both are present)
	hasDep1, hasDep2 := false, false
	for _, d := range dependents {
		if d == 1 {
			hasDep1 = true
		}
		if d == 2 {
			hasDep2 = true
		}
	}
	if !hasDep1 || !hasDep2 {
		t.Errorf("expected dependents 1 and 2, got %v", dependents)
	}
}

func TestPlatformInBaseImage(t *testing.T) {
	t.Parallel()
	content := `FROM --platform=linux/amd64 alpine:3.18
RUN echo "hello"
`
	pr := parseDockerfile(t, content)
	model := NewModel(pr, nil, "Dockerfile")

	info := model.StageInfo(0)
	if info.BaseImage.Platform != "linux/amd64" {
		t.Errorf("expected platform 'linux/amd64', got %q", info.BaseImage.Platform)
	}
}
