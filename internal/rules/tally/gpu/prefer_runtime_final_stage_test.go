package gpu

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferRuntimeFinalStageRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferRuntimeFinalStageRule().Metadata())
}

func TestPreferRuntimeFinalStageRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferRuntimeFinalStageRule(), []testutil.RuleTestCase{
		// --- Should fire ---
		{
			Name: "single stage devel no compile signal",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN pip install torch
CMD ["python", "app.py"]
`,
			WantViolations: 1,
			WantCodes:      []string{PreferRuntimeFinalStageRuleCode},
			WantMessages:   []string{"devel"},
		},
		{
			Name: "multi-stage final devel with COPY --from no compile",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS builder
RUN nvcc -o /app main.cu

FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
COPY --from=builder /app /app
CMD ["/app"]
`,
			WantViolations: 1,
			WantCodes:      []string{PreferRuntimeFinalStageRuleCode},
		},
		{
			Name: "docker.io prefix devel",
			Content: `FROM docker.io/nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN pip install torch
`,
			WantViolations: 1,
		},
		{
			Name: "cudnn-devel variant",
			Content: `FROM nvidia/cuda:12.2.0-cudnn-devel-ubuntu22.04
RUN pip install torch
`,
			WantViolations: 1,
		},
		{
			Name: "devel with only WORKDIR and COPY no compile",
			Content: `FROM nvidia/cuda:12.6.0-devel-ubuntu22.04
WORKDIR /app
COPY . .
RUN pip install -r requirements.txt
CMD ["python", "main.py"]
`,
			WantViolations: 1,
		},

		// --- Should NOT fire: compile signals ---
		{
			Name: "devel with nvcc",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN nvcc -o /app main.cu
`,
			WantViolations: 0,
		},
		{
			Name: "devel with gcc",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN gcc -o /app main.c
`,
			WantViolations: 0,
		},
		{
			Name: "devel with g++",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN g++ -o /app main.cpp
`,
			WantViolations: 0,
		},
		{
			Name: "devel with make",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN make all
`,
			WantViolations: 0,
		},
		{
			Name: "devel with cmake",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN cmake . && make
`,
			WantViolations: 0,
		},
		{
			Name: "devel with ninja",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN ninja -C build
`,
			WantViolations: 0,
		},
		{
			Name: "devel with build-essential package",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y build-essential
`,
			WantViolations: 0,
		},
		{
			Name: "devel with ninja-build package",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y ninja-build
`,
			WantViolations: 0,
		},
		{
			Name: "multi-line RUN with nvcc in continuation",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && \
    apt-get install -y python3 && \
    nvcc -o /app main.cu
`,
			WantViolations: 0,
		},

		// --- Should NOT fire: non-devel variants ---
		{
			Name: "runtime variant",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
CMD ["/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "base variant",
			Content: `FROM nvidia/cuda:12.2.0-base-ubuntu22.04
CMD ["/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "cudnn-runtime variant",
			Content: `FROM nvidia/cuda:12.2.0-cudnn-runtime-ubuntu22.04
CMD ["/app"]
`,
			WantViolations: 0,
		},

		// --- Should NOT fire: non-NVIDIA bases ---
		{
			Name: "ubuntu base",
			Content: `FROM ubuntu:22.04
CMD ["/app"]
`,
			WantViolations: 0,
		},
		{
			Name: "pytorch base",
			Content: `FROM pytorch/pytorch:2.1.0-cuda12.1-cudnn8-runtime
CMD ["/app"]
`,
			WantViolations: 0,
		},

		// --- Should NOT fire: stage ref ---
		{
			Name: "final stage is a stage ref",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS builder
RUN nvcc -o /app main.cu
FROM builder
CMD ["/app"]
`,
			WantViolations: 0,
		},

		// --- Should NOT fire: devel in non-final stage ---
		{
			Name: "devel in builder stage only",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS builder
RUN nvcc -o /app main.cu
FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
COPY --from=builder /app /app
CMD ["/app"]
`,
			WantViolations: 0,
		},
	})
}

func TestPreferRuntimeFinalStageRule_CheckWithoutFacts(t *testing.T) {
	t.Parallel()

	rule := NewPreferRuntimeFinalStageRule()

	t.Run("fires without facts", func(t *testing.T) {
		t.Parallel()
		content := `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN pip install torch
CMD ["python", "app.py"]
`
		input := testutil.MakeLintInput(t, "Dockerfile", content)
		input.Facts = nil

		violations := rule.Check(input)
		if len(violations) != 1 {
			t.Fatalf("expected 1 violation, got %d", len(violations))
		}
		if violations[0].RuleCode != PreferRuntimeFinalStageRuleCode {
			t.Errorf("expected rule %q, got %q", PreferRuntimeFinalStageRuleCode, violations[0].RuleCode)
		}
	})

	t.Run("suppressed by compile command without facts", func(t *testing.T) {
		t.Parallel()
		content := `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN nvcc -o /app main.cu
`
		input := testutil.MakeLintInput(t, "Dockerfile", content)
		input.Facts = nil

		violations := rule.Check(input)
		if len(violations) != 0 {
			t.Fatalf("expected 0 violations, got %d", len(violations))
		}
	})

	t.Run("suppressed by package install without facts", func(t *testing.T) {
		t.Parallel()
		content := `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y build-essential
`
		input := testutil.MakeLintInput(t, "Dockerfile", content)
		input.Facts = nil

		violations := rule.Check(input)
		if len(violations) != 0 {
			t.Fatalf("expected 0 violations, got %d", len(violations))
		}
	})
}

func TestPreferRuntimeFinalStageRule_CheckNilSemantic(t *testing.T) {
	t.Parallel()

	content := `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN pip install torch
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.Semantic = nil

	violations := NewPreferRuntimeFinalStageRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations with nil semantic, got %d", len(violations))
	}
}
