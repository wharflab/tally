package tally

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestCopyFromEmptyScratchStageRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewCopyFromEmptyScratchStageRule().Metadata())
}

func TestCopyFromEmptyScratchStageRule_NoSemantic(t *testing.T) {
	t.Parallel()
	input := testutil.MakeLintInput(t, "Dockerfile", `FROM scratch AS empty

FROM alpine:3.19
COPY --from=empty /app /app
`)
	input.Semantic = nil // explicitly test nil-semantic fallback
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations without semantic model, got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_EmptyScratchWithCopyFrom(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS empty

FROM alpine:3.19
COPY --from=empty /app /app
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]

	t.Run("detection", func(t *testing.T) {
		t.Parallel()
		if v.RuleCode != CopyFromEmptyScratchStageRuleCode {
			t.Errorf("expected rule code %q, got %q", CopyFromEmptyScratchStageRuleCode, v.RuleCode)
		}
		if v.Severity != rules.SeverityError {
			t.Errorf("expected error severity, got %v", v.Severity)
		}
		if !strings.Contains(v.Message, "empty") {
			t.Errorf("message should mention stage name, got %q", v.Message)
		}
		if !strings.Contains(v.Message, "no ADD, COPY, or RUN") {
			t.Errorf("message should explain the problem, got %q", v.Message)
		}
	})

	t.Run("location", func(t *testing.T) {
		t.Parallel()
		// Location should point to the COPY --from instruction (line 4).
		if v.Location.Start.Line != 4 {
			t.Errorf("expected location line 4, got %d", v.Location.Start.Line)
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

func TestCopyFromEmptyScratchStageRule_ScratchWithRUN(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS builder
RUN ["echo", "hello"]

FROM alpine:3.19
COPY --from=builder /out /out
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for scratch stage with RUN, got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_ScratchWithShellFormRUN(t *testing.T) {
	t.Parallel()
	// Shell-form RUN will fail at build time in scratch (no /bin/sh),
	// but copy-from-empty-scratch-stage still does not fire because
	// any RUN counts as a file-producing instruction.
	// The shell-run-in-scratch rule covers this case instead.
	content := `FROM scratch AS builder
RUN echo "will fail"

FROM alpine:3.19
COPY --from=builder /out /out
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations (shell-form RUN is still counted as file-producing), got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_ScratchWithCOPY(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS builder
COPY app /app

FROM alpine:3.19
COPY --from=builder /app /app
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for scratch stage with COPY, got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_ScratchWithADD(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS builder
ADD archive.tar.gz /

FROM alpine:3.19
COPY --from=builder /out /out
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for scratch stage with ADD, got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_NoScratchStages(t *testing.T) {
	t.Parallel()
	content := `FROM golang:1.21 AS builder
RUN go build -o /app

FROM alpine:3.19
COPY --from=builder /app /app
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations without scratch stages, got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_NumericFromRef(t *testing.T) {
	t.Parallel()
	content := `FROM scratch

FROM alpine:3.19
COPY --from=0 /app /app
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for numeric --from referencing empty scratch, got %d", len(violations))
	}

	if !strings.Contains(violations[0].Message, "stage 0") {
		t.Errorf("message should reference stage 0, got %q", violations[0].Message)
	}
}

func TestCopyFromEmptyScratchStageRule_ScratchWithENVOnly(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS config
ENV FOO=bar
LABEL maintainer="test"

FROM alpine:3.19
COPY --from=config /app /app
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for scratch stage with only ENV/LABEL, got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_MultipleCopiesFromSameEmpty(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS empty

FROM alpine:3.19
COPY --from=empty /app /app
COPY --from=empty /config /config
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 2 {
		t.Fatalf("expected 2 violations for two COPYs from same empty scratch, got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_EmptyScratchNotReferenced(t *testing.T) {
	t.Parallel()
	content := `FROM scratch AS empty

FROM alpine:3.19
RUN echo "hello"
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations when empty scratch is not referenced, got %d", len(violations))
	}
}

func TestCopyFromEmptyScratchStageRule_NonScratchDerivedImage(t *testing.T) {
	t.Parallel()
	// "scratch-derived" is not the same as "scratch" - it's an external image name.
	content := `FROM scratch-derived AS empty

FROM alpine:3.19
COPY --from=empty /app /app
`
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", content)
	r := NewCopyFromEmptyScratchStageRule()
	violations := r.Check(input)

	if len(violations) != 0 {
		t.Errorf("expected 0 violations for non-scratch image, got %d", len(violations))
	}
}
