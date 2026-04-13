package gpu

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestCUDAVersionMismatchRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewCUDAVersionMismatchRule().Metadata())
}

func TestCUDAVersionMismatchRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewCUDAVersionMismatchRule(), []testutil.RuleTestCase{
		// --- pip/pip3 index URL mismatches ---
		{
			Name: "cross-major mismatch via index-url (12.1 base, cu118 wheel)",
			Content: `FROM nvidia/cuda:12.1.0-devel-ubuntu20.04
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch torchvision
`,
			WantViolations: 1,
			WantCodes:      []string{CUDAVersionMismatchRuleCode},
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "cross-major mismatch via extra-index-url (12.4 base, cu118 wheel)",
			Content: `FROM nvidia/cuda:12.4.1-cudnn-devel-ubuntu22.04
RUN pip install --extra-index-url https://download.pytorch.org/whl/cu118 xformers
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "extreme version gap (12.6 base, cu118 wheel)",
			Content: `FROM nvidia/cuda:12.6.3-cudnn-runtime-ubuntu24.04
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},

		// --- pip/pip3 package suffix mismatches ---
		{
			Name: "package suffix mismatch (12.2 base, +cu118 suffix)",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN pip3 install torch==2.0.0+cu118
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "wheel minor newer than base (cu124 on 12.1 base)",
			Content: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN pip install torch==2.4.0+cu124
`,
			WantViolations: 1,
			WantMessages:   []string{"cu124"},
		},

		// --- uv ---
		{
			Name: "uv pip install index-url mismatch",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN uv pip install --index-url https://download.pytorch.org/whl/cu118 torch
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "uv torch-backend mismatch",
			Content: `FROM nvidia/cuda:12.4.0-runtime-ubuntu22.04
RUN uv pip install --torch-backend cu118 torch
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "index-url with equals separator",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN pip install --index-url=https://download.pytorch.org/whl/cu118 torch
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "torch-backend with equals separator",
			Content: `FROM nvidia/cuda:12.4.0-runtime-ubuntu22.04
RUN uv pip install --torch-backend=cu118 torch
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},

		// --- conda/mamba/micromamba ---
		{
			Name: "conda pytorch-cuda mismatch (12.2 base, cuda=11.8)",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN conda install -y pytorch pytorch-cuda=11.8 -c pytorch -c nvidia
`,
			WantViolations: 1,
			WantMessages:   []string{"pytorch-cuda=11.8"},
		},
		{
			Name: "mamba cudatoolkit mismatch (12.1 base, cudatoolkit=11.8)",
			Content: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN mamba install cudatoolkit=11.8
`,
			WantViolations: 1,
			WantMessages:   []string{"cudatoolkit=11.8"},
		},

		// --- No-fire cases ---
		{
			Name: "exact match (11.8 base, cu118 wheel)",
			Content: `FROM nvidia/cuda:11.8.0-base-ubuntu22.04
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch
`,
			WantViolations: 0,
		},
		{
			Name: "forward compatible (cu121 on 12.6 base is fine)",
			Content: `FROM nvidia/cuda:12.6.0-runtime-ubuntu22.04
RUN pip install --index-url https://download.pytorch.org/whl/cu121 torch
`,
			WantViolations: 0,
		},
		{
			Name: "non-CUDA base (ubuntu) should not fire",
			Content: `FROM ubuntu:22.04
RUN pip install --index-url https://download.pytorch.org/whl/cu121 torch
`,
			WantViolations: 0,
		},
		{
			Name: "digest ref (no parseable version) should not fire",
			Content: `FROM nvidia/cuda@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
RUN pip install --index-url https://download.pytorch.org/whl/cu121 torch
`,
			WantViolations: 0,
		},
		{
			Name: "no CUDA suffix in pip install should not fire",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN pip install torch==2.1.0
`,
			WantViolations: 0,
		},
		{
			Name: "CPU index URL should not fire",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN pip install --index-url https://download.pytorch.org/whl/cpu torch
`,
			WantViolations: 0,
		},
		{
			Name: "conda matching version should not fire",
			Content: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN conda install -y pytorch pytorch-cuda=12.1 -c pytorch -c nvidia
`,
			WantViolations: 0,
		},
		{
			Name: "conda forward-compat should not fire (12.1 on 12.4 base)",
			Content: `FROM nvidia/cuda:12.4.0-devel-ubuntu22.04
RUN conda install -y pytorch pytorch-cuda=12.1 -c pytorch -c nvidia
`,
			WantViolations: 0,
		},
		{
			Name: "micromamba matching version should not fire",
			Content: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN micromamba install pytorch-cuda=12.1
`,
			WantViolations: 0,
		},

		// --- Cross-cutting ---
		{
			Name: "multi-stage: only fires on CUDA stage with mismatch",
			Content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04 AS builder
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch

FROM ubuntu:22.04 AS runtime
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch
`,
			WantViolations: 1,
		},
		{
			Name: "multi-stage: inherits CUDA version from parent stage",
			Content: `FROM nvidia/cuda:12.4.0-devel-ubuntu22.04 AS builder
RUN nvcc --version

FROM builder AS runner
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "multiple mismatched packages in same RUN",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN pip install torch==2.1.0+cu118 torchvision==0.16.0+cu118
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "continuation lines with mismatch",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN pip install \
    --index-url https://download.pytorch.org/whl/cu118 \
    torch \
    torchvision
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
		{
			Name: "mixed CUDA versions in same RUN both mismatch",
			Content: `FROM nvidia/cuda:11.7.0-runtime-ubuntu22.04
RUN pip install torch==2.0.0+cu118 torchvision==0.16.0+cu121
`,
			WantViolations: 1,
			WantMessages:   []string{"cu118"},
		},
	})
}

func TestCUDAVersionMismatchRule_CheckWithFixes(t *testing.T) {
	t.Parallel()

	rule := NewCUDAVersionMismatchRule()

	tests := []struct {
		name       string
		content    string
		wantFixes  int
		wantSafety rules.FixSafety
	}{
		{
			name: "fix suggestions for cross-major mismatch (base higher)",
			content: `FROM nvidia/cuda:12.4.0-runtime-ubuntu22.04
RUN pip install --index-url https://download.pytorch.org/whl/cu118 torch
`,
			wantFixes:  2,
			wantSafety: rules.FixSuggestion,
		},
		{
			name: "fix suggestions for wheel-newer mismatch (wheel higher)",
			content: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN pip install torch==2.4.0+cu124
`,
			wantFixes:  2,
			wantSafety: rules.FixSuggestion,
		},
		{
			name: "conda fix suggestion",
			content: `FROM nvidia/cuda:12.2.0-devel-ubuntu22.04
RUN conda install -y pytorch pytorch-cuda=11.8 -c pytorch -c nvidia
`,
			wantFixes:  2,
			wantSafety: rules.FixSuggestion,
		},
		{
			name: "no fixes when mismatched refs disagree on CUDA version",
			content: `FROM nvidia/cuda:11.7.0-runtime-ubuntu22.04
RUN pip install torch==2.0.0+cu118 torchvision==0.16.0+cu121
`,
			wantFixes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			violations := rule.Check(input)
			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}
			v := violations[0]
			if len(v.SuggestedFixes) != tt.wantFixes {
				t.Errorf("SuggestedFixes count = %d, want %d", len(v.SuggestedFixes), tt.wantFixes)
			}
			for i, fix := range v.SuggestedFixes {
				if fix.Safety != tt.wantSafety {
					t.Errorf("SuggestedFixes[%d].Safety = %v, want %v", i, fix.Safety, tt.wantSafety)
				}
			}
		})
	}
}

func TestParseCUSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		digits    string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{name: "cu118", digits: "118", wantMajor: 11, wantMinor: 8, wantOK: true},
		{name: "cu121", digits: "121", wantMajor: 12, wantMinor: 1, wantOK: true},
		{name: "cu124", digits: "124", wantMajor: 12, wantMinor: 4, wantOK: true},
		{name: "cu126", digits: "126", wantMajor: 12, wantMinor: 6, wantOK: true},
		{name: "cu128", digits: "128", wantMajor: 12, wantMinor: 8, wantOK: true},
		{name: "cu92", digits: "92", wantMajor: 9, wantMinor: 2, wantOK: true},
		{name: "single digit", digits: "5", wantMajor: 0, wantMinor: 0, wantOK: false},
		{name: "empty", digits: "", wantMajor: 0, wantMinor: 0, wantOK: false},
		{name: "non-numeric", digits: "abc", wantMajor: 0, wantMinor: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			major, minor, ok := parseCUSuffix(tt.digits)
			if ok != tt.wantOK {
				t.Fatalf("parseCUSuffix(%q) ok = %v, want %v", tt.digits, ok, tt.wantOK)
			}
			if major != tt.wantMajor || minor != tt.wantMinor {
				t.Errorf("parseCUSuffix(%q) = (%d, %d), want (%d, %d)",
					tt.digits, major, minor, tt.wantMajor, tt.wantMinor)
			}
		})
	}
}

func TestIsCUDAVersionMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		baseMajor  int
		baseMinor  int
		wheelMajor int
		wheelMinor int
		want       bool
	}{
		{name: "cross-major 12 vs 11", baseMajor: 12, baseMinor: 2, wheelMajor: 11, wheelMinor: 8, want: true},
		{name: "cross-major 11 vs 12", baseMajor: 11, baseMinor: 8, wheelMajor: 12, wheelMinor: 1, want: true},
		{name: "same version", baseMajor: 12, baseMinor: 1, wheelMajor: 12, wheelMinor: 1, want: false},
		{name: "forward compat (wheel older minor)", baseMajor: 12, baseMinor: 6, wheelMajor: 12, wheelMinor: 1, want: false},
		{name: "wheel newer minor", baseMajor: 12, baseMinor: 1, wheelMajor: 12, wheelMinor: 4, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isCUDAVersionMismatch(tt.baseMajor, tt.baseMinor, tt.wheelMajor, tt.wheelMinor)
			if got != tt.want {
				t.Errorf("isCUDAVersionMismatch(%d.%d, %d.%d) = %v, want %v",
					tt.baseMajor, tt.baseMinor, tt.wheelMajor, tt.wheelMinor, got, tt.want)
			}
		})
	}
}

func TestBestCUDASuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		major      int
		minor      int
		wantSuffix string
		wantOK     bool
	}{
		{name: "exact match 12.1", major: 12, minor: 1, wantSuffix: "cu121", wantOK: true},
		{name: "exact match 11.8", major: 11, minor: 8, wantSuffix: "cu118", wantOK: true},
		{name: "between 12.2 rounds down to 12.1", major: 12, minor: 2, wantSuffix: "cu121", wantOK: true},
		{name: "between 12.3 rounds down to 12.1", major: 12, minor: 3, wantSuffix: "cu121", wantOK: true},
		{name: "exact match 12.4", major: 12, minor: 4, wantSuffix: "cu124", wantOK: true},
		{name: "12.5 rounds down to 12.4", major: 12, minor: 5, wantSuffix: "cu124", wantOK: true},
		{name: "12.6 exact", major: 12, minor: 6, wantSuffix: "cu126", wantOK: true},
		{name: "12.8 exact", major: 12, minor: 8, wantSuffix: "cu128", wantOK: true},
		{name: "12.0 has no known suffix", major: 12, minor: 0, wantSuffix: "", wantOK: false},
		{name: "unknown major 13", major: 13, minor: 0, wantSuffix: "", wantOK: false},
		{name: "11.7 exact", major: 11, minor: 7, wantSuffix: "cu117", wantOK: true},
		{name: "11.5 has no known suffix", major: 11, minor: 5, wantSuffix: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			suffix, ok := bestCUDASuffix(tt.major, tt.minor)
			if ok != tt.wantOK {
				t.Fatalf("bestCUDASuffix(%d, %d) ok = %v, want %v", tt.major, tt.minor, ok, tt.wantOK)
			}
			if suffix != tt.wantSuffix {
				t.Errorf("bestCUDASuffix(%d, %d) = %q, want %q", tt.major, tt.minor, suffix, tt.wantSuffix)
			}
		})
	}
}
