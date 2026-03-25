package gpu

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestNoRedundantCUDAInstallRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoRedundantCUDAInstallRule().Metadata())
}

func TestNoRedundantCUDAInstallRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoRedundantCUDAInstallRule(), []testutil.RuleTestCase{
		// --- devel flavor: all CUDA packages are redundant ---
		{
			Name: "devel base with cuda-toolkit install",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 1,
			WantCodes:      []string{NoRedundantCUDAInstallRuleCode},
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "devel base with nvidia-cuda-toolkit",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y nvidia-cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-cuda-toolkit"},
		},
		{
			Name: "devel base with cuda-nvcc",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-nvcc
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-nvcc"},
		},
		{
			Name: "devel base with cuda-nvcc versioned prefix",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-nvcc-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-nvcc-12-2"},
		},
		{
			Name: "devel base with multiple packages",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit cuda-nvcc-12-2 cuda-runtime
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit, cuda-nvcc-12-2, cuda-runtime"},
		},
		{
			Name: "devel base with cuda-libraries prefix",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-libraries-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-libraries-12-2"},
		},

		// --- runtime flavor: runtime+base packages are redundant, devel packages are NOT ---
		{
			Name: "runtime base with cuda-runtime is redundant",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-runtime
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-runtime"},
		},
		{
			Name: "runtime base with cuda-runtime versioned is redundant",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-runtime-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-runtime-12-2"},
		},
		{
			Name: "runtime base with cuda-libraries is redundant",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-libraries-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-libraries-12-2"},
		},
		{
			Name: "runtime base with cuda-compat is redundant",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-compat-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-compat-12-2"},
		},
		{
			Name: "runtime base with cuda-toolkit is NOT redundant",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 0,
		},
		{
			Name: "runtime base with cuda-nvcc is NOT redundant",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-nvcc
`,
			WantViolations: 0,
		},
		{
			Name: "runtime base with libcudnn is NOT redundant",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y libcudnn8
`,
			WantViolations: 0,
		},

		// --- base flavor: only cudart-level packages are redundant ---
		{
			Name: "base flavor with cuda-runtime is redundant",
			Content: `FROM nvidia/cuda:12.2.0-base-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-runtime
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-runtime"},
		},
		{
			Name: "base flavor with cuda-libraries is NOT redundant",
			Content: `FROM nvidia/cuda:12.2.0-base-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-libraries-12-2
`,
			WantViolations: 0,
		},
		{
			Name: "base flavor with cuda-toolkit is NOT redundant",
			Content: `FROM nvidia/cuda:12.2.0-base-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 0,
		},

		// --- cuDNN tags: libcudnn is redundant on cudnn tags ---
		{
			Name: "cudnn-devel base with libcudnn is redundant",
			Content: `FROM nvidia/cuda:12.2.0-cudnn-devel-ubuntu22.04
RUN apt-get update && apt-get install -y libcudnn8
`,
			WantViolations: 1,
			WantMessages:   []string{"libcudnn8"},
		},
		{
			Name: "cudnn-runtime base with libcudnn is redundant",
			Content: `FROM nvidia/cuda:12.2.0-cudnn-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y libcudnn8
`,
			WantViolations: 1,
			WantMessages:   []string{"libcudnn8"},
		},

		// --- tensorrt: never considered redundant (no standard tag includes it) ---
		{
			Name: "devel base with tensorrt is NOT redundant",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y tensorrt
`,
			WantViolations: 0,
		},

		// --- non-CUDA bases: never fire ---
		{
			Name: "nvidia/cuda base with legitimate packages no violation",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y python3 python3-pip curl
`,
			WantViolations: 0,
		},
		{
			Name: "ubuntu base with CUDA install no violation",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y nvidia-cuda-toolkit
`,
			WantViolations: 0,
		},
		{
			Name: "non-nvidia base with cuda package no violation",
			Content: `FROM centos:7
RUN yum install -y cuda-toolkit
`,
			WantViolations: 0,
		},
		{
			Name: "nvcr.io base no violation not nvidia/cuda",
			Content: `FROM nvcr.io/nvidia/pytorch:23.10-py3
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 0,
		},
		{
			Name: "nvidia/cudagl base no violation not nvidia/cuda",
			Content: `FROM nvidia/cudagl:11.4.2-runtime-ubuntu20.04
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 0,
		},

		// --- multi-stage, edge cases ---
		{
			Name: "multi-stage fires only on nvidia/cuda devel stage",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS builder
RUN apt-get update && apt-get install -y cuda-toolkit

FROM ubuntu:22.04 AS runtime
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "FROM stage ref no violation",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS base
RUN apt-get update && apt-get install -y python3

FROM base AS app
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 0,
		},
		{
			Name: "continuation lines on devel",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && \
    apt-get install -y \
    cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "heredoc RUN on devel",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN <<EOF
apt-get update
apt-get install -y cuda-toolkit
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "empty scratch no violation",
			Content: `FROM scratch
`,
			WantViolations: 0,
		},
		{
			Name: "non-CUDA package with cuda substring no false positive",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y barracuda
`,
			WantViolations: 0,
		},
		{
			Name: "docker.io/nvidia/cuda prefix on devel",
			Content: `FROM docker.io/nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "yum install on devel",
			Content: `FROM nvidia/cuda:12.2.0-devel-centos7
RUN yum install -y cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "dnf install on devel",
			Content: `FROM nvidia/cuda:12.2.0-devel-rockylinux9
RUN dnf install -y cuda-toolkit-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit-12-2"},
		},
		{
			Name: "unrecognized tag defaults to devel to avoid false positives",
			Content: `FROM nvidia/cuda:12.2.0
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
	})
}

func TestNoRedundantCUDAInstallRule_CheckWithoutFacts(t *testing.T) {
	t.Parallel()

	content := `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit python3
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.Facts = nil // force fallback path

	rule := NewNoRedundantCUDAInstallRule()
	violations := rule.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].RuleCode != NoRedundantCUDAInstallRuleCode {
		t.Errorf("expected rule %q, got %q", NoRedundantCUDAInstallRuleCode, violations[0].RuleCode)
	}
}

func TestNoRedundantCUDAInstallRule_CheckNilSemantic(t *testing.T) {
	t.Parallel()

	content := `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.Semantic = nil // no semantic model — stageImageInfo returns empty

	violations := NewNoRedundantCUDAInstallRule().Check(input)

	if len(violations) != 0 {
		t.Fatalf("expected 0 violations with nil semantic, got %d", len(violations))
	}
}
