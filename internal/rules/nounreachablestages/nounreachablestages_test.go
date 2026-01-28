package nounreachablestages

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// helper to create LintInput from Dockerfile content
func makeLintInput(t *testing.T, content string) rules.LintInput {
	t.Helper()
	pr, err := dockerfile.Parse(strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	sem := semantic.NewModel(pr, nil, "Dockerfile")
	return rules.LintInput{
		File:     "Dockerfile",
		AST:      pr.AST,
		Stages:   pr.Stages,
		MetaArgs: pr.MetaArgs,
		Source:   pr.Source,
		Semantic: sem,
	}
}

func TestMetadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != "tally/no-unreachable-stages" {
		t.Errorf("expected code 'tally/no-unreachable-stages', got %q", meta.Code)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("expected warning severity, got %v", meta.DefaultSeverity)
	}
	if !meta.EnabledByDefault {
		t.Error("expected rule to be enabled by default")
	}
}

func TestSingleStage_NoViolation(t *testing.T) {
	content := `FROM alpine:3.18
RUN echo "hello"
`
	input := makeLintInput(t, content)
	r := New()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for single stage, got %d", len(violations))
	}
}

func TestAllStagesReachable_NoViolation(t *testing.T) {
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.18
COPY --from=builder /app /app
`
	input := makeLintInput(t, content)
	r := New()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations when all stages reachable, got %d", len(violations))
	}
}

func TestUnreachableNamedStage(t *testing.T) {
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM golang:1.21 AS unused
RUN echo "this is never used"

FROM alpine:3.18
COPY --from=builder /app /app
`
	input := makeLintInput(t, content)
	r := New()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for unreachable stage, got %d", len(violations))
	}

	v := violations[0]
	if v.RuleCode != "tally/no-unreachable-stages" {
		t.Errorf("expected rule code 'tally/no-unreachable-stages', got %q", v.RuleCode)
	}
	if !strings.Contains(v.Message, "unused") {
		t.Errorf("message should mention stage name 'unused', got %q", v.Message)
	}
	if !strings.Contains(v.Message, "index 1") {
		t.Errorf("message should mention index 1, got %q", v.Message)
	}
	if v.Severity != rules.SeverityWarning {
		t.Errorf("expected warning severity, got %v", v.Severity)
	}
	// Should point to line 4 (the FROM for unused stage)
	if v.Location.Start.Line != 4 {
		t.Errorf("expected location line 4, got %d", v.Location.Start.Line)
	}
}

func TestUnreachableUnnamedStage(t *testing.T) {
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM ubuntu:22.04
RUN echo "this is never used"

FROM alpine:3.18
COPY --from=builder /app /app
`
	input := makeLintInput(t, content)
	r := New()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for unreachable stage, got %d", len(violations))
	}

	v := violations[0]
	// Unnamed stage should just show index
	if !strings.Contains(v.Message, "stage 1") {
		t.Errorf("message should mention 'stage 1', got %q", v.Message)
	}
	// Should NOT contain quotes (no name)
	if strings.Contains(v.Message, `"`) {
		t.Errorf("message should not have quoted name for unnamed stage, got %q", v.Message)
	}
}

func TestMultipleUnreachableStages(t *testing.T) {
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM golang:1.21 AS unused1
RUN echo "first unused"

FROM golang:1.21 AS unused2
RUN echo "second unused"

FROM alpine:3.18
COPY --from=builder /app /app
`
	input := makeLintInput(t, content)
	r := New()
	violations := r.Check(input)

	if len(violations) != 2 {
		t.Fatalf("expected 2 violations for 2 unreachable stages, got %d", len(violations))
	}

	// Check both stages are reported
	messages := violations[0].Message + " " + violations[1].Message
	if !strings.Contains(messages, "unused1") {
		t.Error("should report unused1")
	}
	if !strings.Contains(messages, "unused2") {
		t.Error("should report unused2")
	}
}

func TestChainedDependencies_AllReachable(t *testing.T) {
	content := `FROM golang:1.21 AS deps
RUN go mod download

FROM deps AS builder
RUN go build -o /app

FROM alpine:3.18
COPY --from=builder /app /app
`
	input := makeLintInput(t, content)
	r := New()
	violations := r.Check(input)

	// deps is reachable through builder -> final
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for chained dependencies, got %d", len(violations))
		for _, v := range violations {
			t.Logf("  violation: %s", v.Message)
		}
	}
}

func TestFinalStageWithNoDependencies(t *testing.T) {
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.18
RUN echo "final stage with no COPY --from"
`
	input := makeLintInput(t, content)
	r := New()
	violations := r.Check(input)

	// builder is not reachable from final stage
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation when final stage has no COPY --from, got %d", len(violations))
	}

	if !strings.Contains(violations[0].Message, "builder") {
		t.Errorf("should report builder as unreachable, got %q", violations[0].Message)
	}
}

func TestNoSemanticModel_NoViolation(t *testing.T) {
	// Test graceful handling when semantic model is nil
	pr, err := dockerfile.Parse(strings.NewReader(`FROM alpine:3.18
RUN echo "hello"
`))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	input := rules.LintInput{
		File:     "Dockerfile",
		AST:      pr.AST,
		Stages:   pr.Stages,
		MetaArgs: pr.MetaArgs,
		Source:   pr.Source,
		Semantic: nil, // No semantic model
	}

	r := New()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations when semantic model is nil, got %d", len(violations))
	}
}

func TestViolationHasDetail(t *testing.T) {
	content := `FROM golang:1.21 AS unused
RUN echo "never used"

FROM alpine:3.18
RUN echo "final"
`
	input := makeLintInput(t, content)
	r := New()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if violations[0].Detail == "" {
		t.Error("expected violation to have detail/suggestion")
	}
	if !strings.Contains(violations[0].Detail, "COPY --from") {
		t.Errorf("detail should suggest COPY --from, got %q", violations[0].Detail)
	}
}
