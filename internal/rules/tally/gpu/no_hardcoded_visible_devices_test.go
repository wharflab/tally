package gpu

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoHardcodedVisibleDevicesRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoHardcodedVisibleDevicesRule().Metadata())
}

func TestNoHardcodedVisibleDevicesRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoHardcodedVisibleDevicesRule(), []testutil.RuleTestCase{
		// ── NVIDIA_VISIBLE_DEVICES violations ──

		{
			Name: "redundant all on nvidia/cuda base",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=all
`,
			WantViolations: 1,
			WantCodes:      []string{NoHardcodedVisibleDevicesRuleCode},
			WantMessages:   []string{"redundant NVIDIA_VISIBLE_DEVICES=all"},
		},
		{
			Name: "redundant all on docker.io/nvidia/cuda base",
			Content: `FROM docker.io/nvidia/cuda:12.2.0-devel-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=all
`,
			WantViolations: 1,
			WantMessages:   []string{"redundant"},
		},
		{
			Name: "hardcoded single device index",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=0
`,
			WantViolations: 1,
			WantMessages:   []string{"hardcoded GPU device index"},
		},
		{
			Name: "hardcoded multi device index",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=0,1,2
`,
			WantViolations: 1,
			WantMessages:   []string{"hardcoded GPU device index"},
		},
		{
			Name: "hardcoded GPU UUID",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=GPU-aaaabbbb-cccc-dddd-eeee-ffffffffffff
`,
			WantViolations: 1,
			WantMessages:   []string{"hardcoded GPU UUID"},
		},
		{
			Name: "hardcoded MIG UUID",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_VISIBLE_DEVICES=MIG-aaaabbbb-cccc-dddd-eeee-ffffffffffff
`,
			WantViolations: 1,
			WantMessages:   []string{"hardcoded GPU UUID"},
		},
		{
			Name: "device index on non-CUDA base",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_VISIBLE_DEVICES=0
`,
			WantViolations: 1,
			WantMessages:   []string{"hardcoded GPU device index"},
		},

		// ── NVIDIA_VISIBLE_DEVICES no-fire cases ──

		{
			Name: "all on non-CUDA base is intentional",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_VISIBLE_DEVICES=all
`,
			WantViolations: 0,
		},
		{
			Name: "all on nvcr.io NVIDIA base is not flagged",
			Content: `FROM nvcr.io/nvidia/pytorch:24.01-py3
ENV NVIDIA_VISIBLE_DEVICES=all
`,
			WantViolations: 0,
		},
		{
			Name: "none is intentional disable",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=none
`,
			WantViolations: 0,
		},
		{
			Name: "void is intentional disable",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=void
`,
			WantViolations: 0,
		},
		{
			Name: "empty value is not flagged",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=
`,
			WantViolations: 0,
		},
		{
			Name: "variable reference with dollar-brace",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ARG GPU_DEVICES=all
ENV NVIDIA_VISIBLE_DEVICES=${GPU_DEVICES}
`,
			WantViolations: 0,
		},
		{
			Name: "variable reference with dollar",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ARG DEVICES=0
ENV NVIDIA_VISIBLE_DEVICES=$DEVICES
`,
			WantViolations: 0,
		},

		// ── CUDA_VISIBLE_DEVICES violations ──

		{
			Name: "CUDA_VISIBLE_DEVICES hardcoded index",
			Content: `FROM ubuntu:22.04
ENV CUDA_VISIBLE_DEVICES=0
`,
			WantViolations: 1,
			WantCodes:      []string{NoHardcodedVisibleDevicesRuleCode},
			WantMessages:   []string{"hardcoded GPU device index in CUDA_VISIBLE_DEVICES=0"},
		},
		{
			Name: "CUDA_VISIBLE_DEVICES multi index",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV CUDA_VISIBLE_DEVICES=0,1
`,
			WantViolations: 1,
			WantMessages:   []string{"CUDA_VISIBLE_DEVICES=0,1"},
		},
		{
			Name: "CUDA_VISIBLE_DEVICES GPU UUID",
			Content: `FROM ubuntu:22.04
ENV CUDA_VISIBLE_DEVICES=GPU-aaaabbbb-cccc-dddd-eeee-ffffffffffff
`,
			WantViolations: 1,
			WantMessages:   []string{"hardcoded GPU UUID"},
		},
		{
			Name: "CUDA_VISIBLE_DEVICES arbitrary string",
			Content: `FROM ubuntu:22.04
ENV CUDA_VISIBLE_DEVICES=device0
`,
			WantViolations: 1,
			WantMessages:   []string{"CUDA_VISIBLE_DEVICES=device0"},
		},

		// ── CUDA_VISIBLE_DEVICES no-fire cases ──

		{
			Name: "CUDA_VISIBLE_DEVICES empty",
			Content: `FROM ubuntu:22.04
ENV CUDA_VISIBLE_DEVICES=
`,
			WantViolations: 0,
		},
		{
			Name: "CUDA_VISIBLE_DEVICES none",
			Content: `FROM ubuntu:22.04
ENV CUDA_VISIBLE_DEVICES=none
`,
			WantViolations: 0,
		},
		{
			Name: "CUDA_VISIBLE_DEVICES NoDevFiles",
			Content: `FROM ubuntu:22.04
ENV CUDA_VISIBLE_DEVICES=NoDevFiles
`,
			WantViolations: 0,
		},
		{
			Name: "CUDA_VISIBLE_DEVICES variable reference",
			Content: `FROM ubuntu:22.04
ENV CUDA_VISIBLE_DEVICES=$DEVICES
`,
			WantViolations: 0,
		},

		// ── Multi-key ENV ──

		{
			Name: "multi-key ENV with one flagged key",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=all CUDA_HOME=/usr/local/cuda
`,
			WantViolations: 1,
			WantMessages:   []string{"redundant NVIDIA_VISIBLE_DEVICES=all"},
		},

		// ── Multi-stage ──

		{
			Name: "multi-stage fires only in offending stages",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS base
ENV NVIDIA_VISIBLE_DEVICES=all

FROM ubuntu:22.04 AS app
ENV CUDA_VISIBLE_DEVICES=0
`,
			WantViolations: 2,
		},
		{
			Name: "stage reference base treats all as intentional",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS base
RUN echo hello

FROM base AS app
ENV NVIDIA_VISIBLE_DEVICES=all
`,
			// FROM base — base image is a stage ref, stageUsesNVIDIACUDABase returns false,
			// so NVIDIA_VISIBLE_DEVICES=all is classExplicitAll (no violation).
			WantViolations: 0,
		},

		// ── Edge cases ──

		{
			Name: "no ENV instructions",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN echo hello
`,
			WantViolations: 0,
		},
		{
			Name: "quoted value is unquoted before classification",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES="all"
`,
			WantViolations: 1,
			WantMessages:   []string{"redundant"},
		},
		{
			Name: "case-insensitive none",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=None
`,
			WantViolations: 0,
		},
		{
			Name: "both variables in same stage",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=all
ENV CUDA_VISIBLE_DEVICES=0
`,
			WantViolations: 2,
		},
	})
}

func TestNoHardcodedVisibleDevicesRule_FixSafety(t *testing.T) {
	t.Parallel()

	rule := NewNoHardcodedVisibleDevicesRule()

	tests := []struct {
		name       string
		content    string
		wantSafety rules.FixSafety
		wantFix    bool
	}{
		{
			name: "FixSafe for redundant all on nvidia/cuda",
			content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=all
`,
			wantSafety: rules.FixSafe,
			wantFix:    true,
		},
		{
			name: "FixSuggestion for device index",
			content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=0
`,
			wantSafety: rules.FixSuggestion,
			wantFix:    true,
		},
		{
			name: "FixSuggestion for CUDA_VISIBLE_DEVICES",
			content: `FROM ubuntu:22.04
ENV CUDA_VISIBLE_DEVICES=0,1
`,
			wantSafety: rules.FixSuggestion,
			wantFix:    true,
		},
		{
			name: "FixSuggestion for GPU UUID",
			content: `FROM ubuntu:22.04
ENV NVIDIA_VISIBLE_DEVICES=GPU-aaaa-bbbb-cccc-dddd
`,
			wantSafety: rules.FixSuggestion,
			wantFix:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			violations := rule.Check(input)
			if len(violations) != 1 {
				t.Fatalf("expected 1 violation, got %d", len(violations))
			}
			v := violations[0]
			if tt.wantFix && v.SuggestedFix == nil {
				t.Fatal("expected SuggestedFix, got nil")
			}
			if v.SuggestedFix != nil && v.SuggestedFix.Safety != tt.wantSafety {
				t.Errorf("SuggestedFix.Safety = %v, want %v", v.SuggestedFix.Safety, tt.wantSafety)
			}
		})
	}
}

// ── Classification helper tests ──

func TestClassifyValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		key        string
		value      string
		isCUDABase bool
		want       visibleDevicesClass
	}{
		{"empty", "NVIDIA_VISIBLE_DEVICES", "", false, classNone},
		{"none", "NVIDIA_VISIBLE_DEVICES", "none", false, classNone},
		{"None-mixed", "NVIDIA_VISIBLE_DEVICES", "None", false, classNone},
		{"void", "NVIDIA_VISIBLE_DEVICES", "void", false, classNone},
		{"variable-brace", "NVIDIA_VISIBLE_DEVICES", "${GPU}", false, classNone},
		{"variable-dollar", "NVIDIA_VISIBLE_DEVICES", "$DEVICES", false, classNone},

		{"all-on-cuda", "NVIDIA_VISIBLE_DEVICES", "all", true, classRedundantAll},
		{"all-on-non-cuda", "NVIDIA_VISIBLE_DEVICES", "all", false, classExplicitAll},

		{"single-index", "NVIDIA_VISIBLE_DEVICES", "0", false, classHardcodedIndex},
		{"multi-index", "NVIDIA_VISIBLE_DEVICES", "0,1,2", false, classHardcodedIndex},
		{"spaced-index", "NVIDIA_VISIBLE_DEVICES", "0, 1", false, classHardcodedIndex},

		{"gpu-uuid", "NVIDIA_VISIBLE_DEVICES", "GPU-aaaa-bbbb", false, classHardcodedUUID},
		{"mig-uuid", "NVIDIA_VISIBLE_DEVICES", "MIG-aaaa-bbbb", false, classHardcodedUUID},
		{"gpu-uuid-lower", "NVIDIA_VISIBLE_DEVICES", "gpu-aaaa-bbbb", false, classHardcodedUUID},

		{"cuda-vis-none", "CUDA_VISIBLE_DEVICES", "none", false, classNone},
		{"cuda-vis-nodevfiles", "CUDA_VISIBLE_DEVICES", "NoDevFiles", false, classNone},
		{"cuda-vis-index", "CUDA_VISIBLE_DEVICES", "0", false, classHardcodedIndex},
		{"cuda-vis-uuid", "CUDA_VISIBLE_DEVICES", "GPU-uuid", false, classHardcodedUUID},
		{"cuda-vis-other", "CUDA_VISIBLE_DEVICES", "device0", false, classHardcodedOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyValue(tt.key, tt.value, tt.isCUDABase)
			if got != tt.want {
				t.Errorf("classifyValue(%q, %q, %v) = %d, want %d", tt.key, tt.value, tt.isCUDABase, got, tt.want)
			}
		})
	}
}

func TestIsDeviceIndexList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  bool
	}{
		{"0", true},
		{"1", true},
		{"0,1", true},
		{"0,1,2,3", true},
		{"0, 1, 2", true},
		{"", false},
		{"all", false},
		{"GPU-abc", false},
		{"none", false},
		{",", false},
		{"0a", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			got := isDeviceIndexList(tt.value)
			if got != tt.want {
				t.Errorf("isDeviceIndexList(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestIsGPUOrMIGUUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  bool
	}{
		{"GPU-aaaa-bbbb-cccc", true},
		{"MIG-aaaa-bbbb-cccc", true},
		{"gpu-lower-case", true},
		{"mig-lower-case", true},
		{"0", false},
		{"all", false},
		{"none", false},
		{"GPUAAAA", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			got := isGPUOrMIGUUID(tt.value)
			if got != tt.want {
				t.Errorf("isGPUOrMIGUUID(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
