package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestCircularStageDepsRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewCircularStageDepsRule().Metadata())
}

func TestCircularStageDepsRule_NoSemantic(t *testing.T) {
	t.Parallel()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM alpine:3.19
RUN echo "hello"
`)
	r := NewCircularStageDepsRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations without semantic model, got %d", len(violations))
	}
}

func TestCircularStageDepsRule_SingleStage(t *testing.T) {
	t.Parallel()
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", `FROM alpine:3.19
RUN echo "hello"
`)
	r := NewCircularStageDepsRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for single stage, got %d", len(violations))
	}
}

func TestCircularStageDepsRule_NoCycle(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.19
COPY --from=builder /app /app
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCircularStageDepsRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for valid multi-stage build, got %d", len(violations))
	}
}

func TestCircularStageDepsRule_TwoStageCycle(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.19 AS a
COPY --from=b /x /x

FROM alpine:3.19 AS b
COPY --from=a /y /y

FROM alpine:3.19
COPY --from=a /x /final
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCircularStageDepsRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for 2-stage cycle, got %d", len(violations))
	}

	v := violations[0]

	t.Run("detection", func(t *testing.T) {
		t.Parallel()
		if v.RuleCode != CircularStageDepsRuleCode {
			t.Errorf("expected rule code %q, got %q", CircularStageDepsRuleCode, v.RuleCode)
		}
		if v.Severity != rules.SeverityError {
			t.Errorf("expected error severity, got %v", v.Severity)
		}
		if !strings.Contains(v.Message, `"a"`) || !strings.Contains(v.Message, `"b"`) {
			t.Errorf("message should mention stage names, got %q", v.Message)
		}
		if !strings.Contains(v.Message, "→") {
			t.Errorf("message should contain arrow notation, got %q", v.Message)
		}
	})

	t.Run("location", func(t *testing.T) {
		t.Parallel()
		// Location should point to the FROM of the lowest-indexed stage (line 1).
		if v.Location.Start.Line != 1 {
			t.Errorf("expected location line 1, got %d", v.Location.Start.Line)
		}
	})

	t.Run("metadata", func(t *testing.T) {
		t.Parallel()
		if v.Detail == "" {
			t.Error("expected violation to have detail")
		}
		if v.DocURL == "" {
			t.Error("expected violation to have DocURL")
		}
	})
}

func TestCircularStageDepsRule_ThreeStageCycle(t *testing.T) {
	t.Parallel()
	content := `FROM alpine:3.19 AS a
COPY --from=c /z /z

FROM alpine:3.19 AS b
COPY --from=a /x /x

FROM alpine:3.19 AS c
COPY --from=b /y /y

FROM alpine:3.19
COPY --from=a /x /final
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCircularStageDepsRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for 3-stage cycle, got %d", len(violations))
	}

	v := violations[0]
	// All three stages should be mentioned.
	if !strings.Contains(v.Message, `"a"`) || !strings.Contains(v.Message, `"b"`) || !strings.Contains(v.Message, `"c"`) {
		t.Errorf("message should mention all 3 stage names, got %q", v.Message)
	}
}

func TestCircularStageDepsRule_NoCycleWithChainedDeps(t *testing.T) {
	t.Parallel()
	// Long chain: deps → builder → runtime (no cycle)
	content := `FROM golang:1.21 AS deps
RUN go mod download

FROM deps AS builder
RUN go build -o /app

FROM alpine:3.19
COPY --from=builder /app /app
CMD ["/app"]
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCircularStageDepsRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for chained deps, got %d", len(violations))
	}
}

func TestCircularStageDepsRule_UnnamedStages(t *testing.T) {
	t.Parallel()
	// Cycle with numeric references: stage 0 copies from stage 1, stage 1 copies from stage 0.
	content := `FROM alpine:3.19
COPY --from=1 /x /x

FROM alpine:3.19
COPY --from=0 /y /y

FROM alpine:3.19
RUN echo "final"
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCircularStageDepsRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for numeric ref cycle, got %d", len(violations))
	}

	v := violations[0]
	// Should use "stage N" format for unnamed stages.
	if !strings.Contains(v.Message, "stage 0") || !strings.Contains(v.Message, "stage 1") {
		t.Errorf("message should use 'stage N' for unnamed stages, got %q", v.Message)
	}
}
