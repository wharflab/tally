package gpu

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/testutil"
)

func TestNoContainerRuntimeInImageRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoContainerRuntimeInImageRule().Metadata())
}

func TestNoContainerRuntimeInImageRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoContainerRuntimeInImageRule(), []testutil.RuleTestCase{
		{
			Name: "apt-get install nvidia-container-toolkit",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y nvidia-container-toolkit
`,
			WantViolations: 1,
			WantCodes:      []string{NoContainerRuntimeInImageRuleCode},
			WantMessages:   []string{"nvidia-container-toolkit"},
		},
		{
			Name: "yum install nvidia-docker2",
			Content: `FROM centos:7
RUN yum install -y nvidia-docker2
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-docker2"},
		},
		{
			Name: "apt-get install libnvidia-container1 prefix match",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y libnvidia-container1
`,
			WantViolations: 1,
			WantMessages:   []string{"libnvidia-container1"},
		},
		{
			Name: "dnf install libnvidia-container-tools prefix match",
			Content: `FROM fedora:38
RUN dnf install -y libnvidia-container-tools
`,
			WantViolations: 1,
			WantMessages:   []string{"libnvidia-container-tools"},
		},
		{
			Name: "microdnf install nvidia-container-toolkit",
			Content: `FROM registry.access.redhat.com/ubi9/ubi-minimal:9.3
RUN microdnf install -y nvidia-container-toolkit && microdnf clean all
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-container-toolkit"},
		},
		{
			Name: "apk add nvidia-container-toolkit",
			Content: `FROM alpine:3.18
RUN apk add --no-cache nvidia-container-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-container-toolkit"},
		},
		{
			Name: "multiple runtime packages in one RUN",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y nvidia-container-toolkit nvidia-docker2 libnvidia-container1
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-container-toolkit, nvidia-docker2, libnvidia-container1"},
		},
		{
			Name: "non-GPU packages no violation",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl wget git
`,
			WantViolations: 0,
		},
		{
			Name: "CUDA base with legitimate packages no violation",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN apt-get update && apt-get install -y python3 python3-pip
`,
			WantViolations: 0,
		},
		{
			Name: "runtime package in CMD no violation",
			Content: `FROM ubuntu:22.04
CMD ["nvidia-container-toolkit", "--version"]
`,
			WantViolations: 0,
		},
		{
			Name: "multi-stage only flags offending stage",
			Content: `FROM ubuntu:22.04 AS base
RUN apt-get update && apt-get install -y curl

FROM ubuntu:22.04 AS runtime
RUN apt-get update && apt-get install -y nvidia-container-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-container-toolkit"},
		},
		{
			Name: "empty scratch no violation",
			Content: `FROM scratch
`,
			WantViolations: 0,
		},
		{
			Name: "continuation lines",
			Content: `FROM ubuntu:22.04
RUN apt-get update && \
    apt-get install -y \
    nvidia-container-toolkit
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-container-toolkit"},
		},
		{
			Name: "heredoc RUN",
			Content: `FROM ubuntu:22.04
RUN <<EOF
apt-get update
apt-get install -y nvidia-container-toolkit
EOF
`,
			WantViolations: 1,
			WantMessages:   []string{"nvidia-container-toolkit"},
		},
		{
			Name: "package name as substring no false positive",
			Content: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y some-nvidia-container-toolkit-wrapper
`,
			WantViolations: 0,
		},
	})
}
