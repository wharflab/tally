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
		{
			Name: "nvidia/cuda base with cuda-toolkit install",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 1,
			WantCodes:      []string{NoRedundantCUDAInstallRuleCode},
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "nvidia/cuda base with nvidia-cuda-toolkit",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y nvidia-cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-cuda-toolkit"},
		},
		{
			Name: "nvidia/cuda base with libcudnn prefix match",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y libcudnn8
`,
			WantViolations: 1,
			WantMessages:   []string{"libcudnn8"},
		},
		{
			Name: "nvidia/cuda base with tensorrt prefix match",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y tensorrt
`,
			WantViolations: 1,
			WantMessages:   []string{"tensorrt"},
		},
		{
			Name: "nvidia/cuda base with cuda-compat prefix",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-compat-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-compat-12-2"},
		},
		{
			Name: "nvidia/cuda base with cuda-runtime prefix",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-runtime-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-runtime-12-2"},
		},
		{
			Name: "nvidia/cuda base with cuda-libraries prefix",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-libraries-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-libraries-12-2"},
		},
		{
			Name: "nvidia/cuda base with cuda-nvcc prefix",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-nvcc-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-nvcc-12-2"},
		},
		{
			Name: "nvidia/cuda base with multiple CUDA packages in one RUN",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit libcudnn8 tensorrt
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit, libcudnn8, tensorrt"},
		},
		{
			Name: "nvidia/cuda base with yum install",
			Content: `FROM nvidia/cuda:12.2.0-runtime-centos7
RUN yum install -y cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "nvidia/cuda base with dnf install",
			Content: `FROM nvidia/cuda:12.2.0-runtime-rockylinux9
RUN dnf install -y cuda-toolkit-12-2
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit-12-2"},
		},
		{
			Name: "docker.io/nvidia/cuda prefix",
			Content: `FROM docker.io/nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
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
		{
			Name: "multi-stage fires only on nvidia/cuda stage",
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
			// Stage ref — stageUsesNVIDIACUDABase returns false for stage refs.
			// The base image is not directly nvidia/cuda but a reference to another stage.
			WantViolations: 0,
		},
		{
			Name: "continuation lines",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && \
    apt-get install -y \
    cuda-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-toolkit"},
		},
		{
			Name: "heredoc RUN",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
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
			Name: "exact match cuda",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda"},
		},
		{
			Name: "exact match cuda-runtime",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-runtime
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-runtime"},
		},
		{
			Name: "exact match cuda-nvcc",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-nvcc
`,
			WantViolations: 1,
			WantMessages:   []string{"cuda-nvcc"},
		},
		{
			Name: "non-CUDA package with cuda substring no false positive",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y barracuda
`,
			WantViolations: 0,
		},
	})
}

func TestNoRedundantCUDAInstallRule_CheckWithoutFacts(t *testing.T) {
	t.Parallel()

	content := `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
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

	content := `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y cuda-toolkit
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	input.Semantic = nil // no semantic model — stageIsGated returns false

	violations := NewNoRedundantCUDAInstallRule().Check(input)

	if len(violations) != 0 {
		t.Fatalf("expected 0 violations with nil semantic, got %d", len(violations))
	}
}
