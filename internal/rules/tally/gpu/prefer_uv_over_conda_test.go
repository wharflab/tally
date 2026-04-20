package gpu

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/facts"
	patchutil "github.com/wharflab/tally/internal/patch"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

// fakeBuildContext is a minimal rules.BuildContext for tests that need
// COPY sources to be observable (e.g., environment.yml suppression).
type fakeBuildContext struct {
	files map[string]bool
}

func (f *fakeBuildContext) IsIgnored(string) (bool, error)  { return false, nil }
func (f *fakeBuildContext) FileExists(path string) bool     { return f.files[path] }
func (f *fakeBuildContext) ReadFile(string) ([]byte, error) { return nil, nil }
func (f *fakeBuildContext) IsHeredocFile(string) bool       { return false }
func (f *fakeBuildContext) HasIgnoreFile() bool             { return false }
func (f *fakeBuildContext) HasIgnoreExclusions() bool       { return false }

func TestPreferUVOverCondaRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferUVOverCondaRule().Metadata())
}

func TestPreferUVOverCondaRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferUVOverCondaRule(), []testutil.RuleTestCase{
		{
			Name: "fires on nvidia/cuda + conda install pytorch",
			Content: `FROM nvidia/cuda:12.1.0-devel-ubuntu22.04
RUN conda install -y pytorch pytorch-cuda=12.1 -c pytorch -c nvidia
CMD ["python"]
`,
			WantViolations: 1,
			WantCodes:      []string{PreferUVOverCondaRuleCode},
			WantMessages:   []string{"conda"},
		},
		{
			Name: "fires on pytorch/pytorch base + mamba install transformers",
			Content: `FROM pytorch/pytorch:2.1.0-cuda12.1-cudnn8-devel
RUN mamba install -y numpy transformers
`,
			WantViolations: 1,
		},
		{
			Name: "fires on nvidia/cuda + micromamba install of gpu-only extensions",
			Content: `FROM nvidia/cuda:12.4.0-devel-ubuntu22.04
RUN micromamba install -y flash-attn xformers
`,
			WantViolations: 1,
		},
		{
			Name: "fires once per stage even with multiple qualifying RUNs",
			Content: `FROM nvidia/cuda:12.1.0-devel-ubuntu22.04
RUN conda install -y numpy
RUN mamba install -y torch
`,
			WantViolations: 1,
		},
		{
			Name: "multi-stage: inherits CUDA from builder; child-stage conda install fires",
			Content: `FROM nvidia/cuda:12.4.0-devel-ubuntu22.04 AS builder
RUN nvcc --version

FROM builder AS runner
RUN conda install -y torch torchvision
CMD ["python"]
`,
			WantViolations: 1,
		},
		{
			Name: "does not fire on CPU base even with conda install",
			Content: `FROM ubuntu:22.04
RUN conda install -y numpy torch
`,
			WantViolations: 0,
		},
		{
			Name: "does not fire when conda installs only system packages",
			Content: `FROM nvidia/cuda:12.1.0-devel-ubuntu22.04
RUN conda install -y gcc cmake
`,
			WantViolations: 0,
		},
		{
			Name: "does not fire when stage uses uv already",
			Content: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN uv pip install --system torch numpy
`,
			WantViolations: 0,
		},
		{
			Name: "heavy env workflow via conda env create suppresses the rule",
			Content: `FROM nvidia/cuda:12.1.0-devel-ubuntu22.04
COPY . /app
RUN conda env create -f environment.yml
RUN conda install -y numpy
`,
			WantViolations: 0,
		},
		{
			Name: "continuation lines still detected",
			Content: `FROM nvidia/cuda:12.1.0-devel-ubuntu22.04
RUN conda install -y \
    numpy \
    transformers \
    torch
`,
			WantViolations: 1,
		},
		{
			Name: "heredoc RUN script still detected",
			Content: `FROM nvidia/cuda:12.1.0-devel-ubuntu22.04
RUN <<EOF
conda install -y numpy torch
EOF
`,
			WantViolations: 1,
		},
		{
			Name: "mixed: CPU stage + GPU stage only fires on GPU stage",
			Content: `FROM ubuntu:22.04 AS base
RUN conda install -y numpy

FROM nvidia/cuda:12.1.0-devel-ubuntu22.04 AS gpu
RUN conda install -y torch
`,
			WantViolations: 1,
		},
	})
}

// TestPreferUVOverCondaRule_EnvYmlInContextSuppresses verifies the
// environment.yml suppression path that requires an observable build context.
func TestPreferUVOverCondaRule_EnvYmlInContextSuppresses(t *testing.T) {
	t.Parallel()

	ctx := &fakeBuildContext{files: map[string]bool{
		"environment.yml": true,
	}}
	content := `FROM nvidia/cuda:12.1.0-devel-ubuntu22.04
COPY environment.yml /app/
RUN conda install -y torch
`
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", content, ctx)
	violations := NewPreferUVOverCondaRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected suppression when environment.yml is present, got %d violations", len(violations))
	}
}

// TestPreferUVOverCondaRule_CheckFixAttached verifies the async AI fix is
// attached with the correct resolver, objective kind, and safety.
func TestPreferUVOverCondaRule_CheckFixAttached(t *testing.T) {
	t.Parallel()

	content := `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN conda install -y numpy torch
CMD ["python"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewPreferUVOverCondaRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("want 1 violation, got %d", len(violations))
	}
	v := violations[0]
	fix := v.SuggestedFix
	if fix == nil {
		t.Fatal("SuggestedFix is nil")
	}
	if fix.Safety != rules.FixUnsafe {
		t.Errorf("Safety = %v, want FixUnsafe", fix.Safety)
	}
	if !fix.NeedsResolve {
		t.Error("NeedsResolve = false, want true")
	}
	if fix.ResolverID != autofixdata.ResolverID {
		t.Errorf("ResolverID = %q, want %q", fix.ResolverID, autofixdata.ResolverID)
	}
	if fix.Priority != 150 {
		t.Errorf("Priority = %d, want 150", fix.Priority)
	}
	req, ok := fix.ResolverData.(*autofixdata.ObjectiveRequest)
	if !ok || req == nil {
		t.Fatalf("ResolverData = %T, want *autofixdata.ObjectiveRequest", fix.ResolverData)
	}
	if req.Kind != autofixdata.ObjectiveUVOverConda {
		t.Errorf("ObjectiveRequest.Kind = %q, want %q", req.Kind, autofixdata.ObjectiveUVOverConda)
	}
	if len(req.Signals) == 0 {
		t.Error("ObjectiveRequest.Signals is empty")
	}
	if req.Signals[0].Manager != "conda" {
		t.Errorf("Signals[0].Manager = %q, want %q", req.Signals[0].Manager, "conda")
	}
	if !contains(req.Signals[0].Packages, "torch") {
		t.Errorf("Signals[0].Packages missing 'torch': %v", req.Signals[0].Packages)
	}
	// StageIndex should track the final stage (0 in a single-stage Dockerfile).
	if v.StageIndex != 0 {
		t.Errorf("StageIndex = %d, want 0", v.StageIndex)
	}
}

func TestNormalizeCondaPackageName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"torch":             "torch",
		"Torch":             "torch",
		"torch==2.0.0":      "torch",
		"pytorch-cuda=12.1": "pytorch-cuda",
		"numpy>=1.20":       "numpy",
		"scipy<2":           "scipy",
		"pkg!=1":            "pkg",
		"pkg 1":             "pkg",
		"":                  "",
		"  ":                "",
		// Conda MatchSpec channel-qualified forms (see conda/models/match_spec).
		"conda-forge::torch":          "torch",
		"conda-forge/linux-64::torch": "torch",
		"defaults::numpy=1.20":        "numpy",
		"nvidia::pytorch-cuda=12.1":   "pytorch-cuda",
	}
	for in, want := range cases {
		if got := normalizeCondaPackageName(in); got != want {
			t.Errorf("normalizeCondaPackageName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCondaManagers(t *testing.T) {
	t.Parallel()

	for _, m := range []string{"conda", "mamba", "micromamba"} {
		if !condaManagers[m] {
			t.Errorf("condaManagers[%q] = false, want true", m)
		}
	}
	for _, m := range []string{"", "pip", "pip3", "uv", "apt", "apt-get"} {
		if condaManagers[m] {
			t.Errorf("condaManagers[%q] = true, want false", m)
		}
	}
}

func TestBasenameLower(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                     "",
		"Dockerfile":           "dockerfile",
		"/a/b/ENV.yml":         "env.yml",
		"a/b/environment.YAML": "environment.yaml",
	}
	for in, want := range cases {
		if got := basenameLower(in); got != want {
			t.Errorf("basenameLower(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- Objective tests ---

func TestUVOverCondaObjective_Kind(t *testing.T) {
	t.Parallel()

	o := &uvOverCondaObjective{}
	if got := o.Kind(); got != autofixdata.ObjectiveUVOverConda {
		t.Errorf("Kind() = %q, want %q", got, autofixdata.ObjectiveUVOverConda)
	}
}

func TestUVOverCondaObjective_BuildPrompt(t *testing.T) {
	t.Parallel()

	o := &uvOverCondaObjective{}
	src := []byte(`FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN conda install -y numpy torch
CMD ["python"]
`)
	prompt, err := o.BuildPrompt(autofixdata.PromptContext{
		FilePath: "Dockerfile",
		Source:   src,
		Request: &autofixdata.ObjectiveRequest{
			Kind: autofixdata.ObjectiveUVOverConda,
			File: "Dockerfile",
			Signals: []autofixdata.Signal{{
				Kind:     autofixdata.SignalKindPackageInstall,
				Manager:  "conda",
				Packages: []string{"numpy", "torch"},
				Line:     2,
			}},
		},
		AbsPath: "/tmp/Dockerfile",
		Mode:    autofixdata.OutputPatch,
	})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	for _, want := range []string{
		"migrate the Dockerfile below from conda/mamba/micromamba to uv",
		"Input Dockerfile (Dockerfile,",
		"Signals (pointers)",
		"conda",
		"Output format:",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("BuildPrompt output missing %q; full prompt:\n%s", want, prompt)
		}
	}
}

func TestUVOverCondaObjective_BuildPrompt_IncludesCUDAVersion(t *testing.T) {
	t.Parallel()

	o := &uvOverCondaObjective{}
	src := []byte("FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04\nRUN conda install torch\n")
	prompt, err := o.BuildPrompt(autofixdata.PromptContext{
		FilePath: "Dockerfile",
		Source:   src,
		Request: &autofixdata.ObjectiveRequest{
			Kind: autofixdata.ObjectiveUVOverConda,
			File: "Dockerfile",
			Facts: map[string]any{
				"cuda-major": 12,
				"cuda-minor": 1,
			},
		},
		Mode: autofixdata.OutputPatch,
	})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	for _, want := range []string{
		"Base image CUDA version: 12.1",
		"cu121",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("BuildPrompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestUVOverCondaObjective_BuildPrompt_SkipsCUDAHintWhenAbsent(t *testing.T) {
	t.Parallel()

	o := &uvOverCondaObjective{}
	src := []byte("FROM pytorch/pytorch:2.1.0-cuda12.1-cudnn8-devel\nRUN conda install torch\n")
	prompt, err := o.BuildPrompt(autofixdata.PromptContext{
		FilePath: "Dockerfile",
		Source:   src,
		Request: &autofixdata.ObjectiveRequest{
			Kind: autofixdata.ObjectiveUVOverConda,
			File: "Dockerfile",
			// No cuda-major/cuda-minor — e.g. pytorch/pytorch base, facts carry 0.
			Facts: map[string]any{"cuda-major": 0, "cuda-minor": 0},
		},
		Mode: autofixdata.OutputPatch,
	})
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if strings.Contains(prompt, "Base image CUDA version:") {
		t.Errorf("BuildPrompt should omit CUDA hint when cuda-major is 0:\n%s", prompt)
	}
}

func TestUVOverCondaObjective_BuildRetryPrompt(t *testing.T) {
	t.Parallel()

	o := &uvOverCondaObjective{}
	prompt, err := o.BuildRetryPrompt(autofixdata.RetryPromptContext{
		FilePath: "Dockerfile",
		Proposed: []byte("FROM alpine:3.20\nCMD [\"sh\"]\n"),
		BlockingIssues: []autofixdata.BlockingIssue{{
			Rule:    "runtime",
			Message: "proposed Dockerfile dropped CMD from the final stage",
		}},
		Mode: autofixdata.OutputPatch,
	})
	if err != nil {
		t.Fatalf("BuildRetryPrompt: %v", err)
	}
	for _, want := range []string{
		"previously produced a Dockerfile migrating conda to uv",
		"Blocking issues (JSON)",
		"Do not re-introduce",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("BuildRetryPrompt output missing %q", want)
		}
	}
}

func TestUVOverCondaObjective_BuildSimplifiedPrompt(t *testing.T) {
	t.Parallel()

	o := &uvOverCondaObjective{}
	prompt := o.BuildSimplifiedPrompt(autofixdata.SimplifiedPromptContext{
		FilePath: "Dockerfile",
		Source:   []byte("FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04\nRUN conda install torch\nCMD [\"python\"]\n"),
		Mode:     autofixdata.OutputDockerfile,
	})
	for _, want := range []string{
		"Migrate the Dockerfile below from conda/mamba/micromamba to uv",
		"NO_CHANGE",
		"Dockerfile fenced code block",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("BuildSimplifiedPrompt missing %q", want)
		}
	}

	// Also exercise the patch-mode fallback for coverage.
	patch := o.BuildSimplifiedPrompt(autofixdata.SimplifiedPromptContext{
		FilePath: "sub/Dockerfile",
		Source:   []byte("FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04\nRUN conda install torch\n"),
		Mode:     autofixdata.OutputPatch,
	})
	if !strings.Contains(patch, "diff fenced code block") {
		t.Errorf("BuildSimplifiedPrompt(patch) missing diff hint")
	}
}

func TestUVOverCondaObjective_ValidateProposal(t *testing.T) {
	t.Parallel()

	orig := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=bar
RUN conda install -y numpy torch
CMD ["python", "-m", "app"]
`)

	t.Run("happy path: conda replaced with uv, runtime preserved", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=bar
RUN pip install uv && uv pip install --system numpy torch
CMD ["python", "-m", "app"]
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
		if len(blocking) != 0 {
			t.Errorf("expected no blocking issues, got %v", blocking)
		}
	})

	t.Run("blocks when conda install of ML package remains", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=bar
RUN conda install -y numpy torch
CMD ["python", "-m", "app"]
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
		if !containsRule(blocking, "migration") {
			t.Errorf("expected migration blocking issue, got %v", blocking)
		}
	})

	t.Run("blocks when ML packages are deleted instead of migrated", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=bar
RUN echo "nothing installed"
CMD ["python", "-m", "app"]
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
		if !containsRule(blocking, "migration") {
			t.Errorf("expected migration blocking issue for deleted packages, got %v", blocking)
		}
		// The validator should name the dropped packages deterministically.
		msgs := strings.Join(issueMessages(blocking), "\n")
		for _, want := range []string{"numpy", "torch"} {
			if !strings.Contains(msgs, want) {
				t.Errorf("expected dropped-package message for %q in %s", want, msgs)
			}
		}
	})

	t.Run("blocks when proposal introduces conda env create", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=bar
RUN conda env create -f /app/env.yml
CMD ["python", "-m", "app"]
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
		if !containsRule(blocking, "migration") {
			t.Errorf("expected migration blocking issue for introduced env create, got %v", blocking)
		}
		msgs := strings.Join(issueMessages(blocking), "\n")
		if !strings.Contains(msgs, "conda env create") {
			t.Errorf("expected env-create message, got %s", msgs)
		}
	})

	t.Run("allows migration to uv pip install", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=bar
RUN pip install uv && uv pip install --system numpy torch
CMD ["python", "-m", "app"]
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
		if containsRule(blocking, "migration") {
			t.Errorf("uv pip install should satisfy migration; got %v", blocking)
		}
	})

	t.Run("blocks when CMD is dropped", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=bar
RUN uv pip install --system numpy torch
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
		if !containsRule(blocking, "runtime") {
			t.Errorf("expected runtime blocking issue, got %v", blocking)
		}
	})

	t.Run("blocks when WORKDIR changes", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /other
ENV FOO=bar
RUN uv pip install --system numpy torch
CMD ["python", "-m", "app"]
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
		if !containsRule(blocking, "runtime") {
			t.Errorf("expected runtime blocking issue for WORKDIR, got %v", blocking)
		}
	})

	t.Run("blocks when ENV is changed", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=baz
RUN uv pip install --system numpy torch
CMD ["python", "-m", "app"]
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
		if !containsRule(blocking, "runtime") {
			t.Errorf("expected runtime blocking issue for ENV, got %v", blocking)
		}
	})

	t.Run("does not block if conda install has no ML package", func(t *testing.T) {
		t.Parallel()
		sysOrig := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
CMD ["python"]
`)
		sysProposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN conda install -y gcc cmake
CMD ["python"]
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, sysOrig, sysProposed)
		for _, b := range blocking {
			if b.Rule == "migration" {
				t.Errorf("unexpected migration block for system conda install: %v", b)
			}
		}
	})

	t.Run("handles nil parse results", func(t *testing.T) {
		t.Parallel()
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, nil, nil)
		if len(blocking) == 0 {
			t.Error("expected blocking issue for nil parse results")
		}
	})
}

// TestUVOverCondaObjective_ValidateProposal_Exhaustive drives coverage of
// the individual runtime-invariant validators (ENTRYPOINT, USER, EXPOSE,
// WORKDIR, LABEL, HEALTHCHECK) both for "added/dropped" and "changed"
// detection and for the happy "identical" path.
func TestUVOverCondaObjective_ValidateProposal_Exhaustive(t *testing.T) {
	t.Parallel()

	baseOrig := `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
ENTRYPOINT ["/entry.sh"]
USER app
EXPOSE 8080
LABEL foo=bar
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
CMD ["python", "-m", "app"]
`
	orig := mustParse(t, baseOrig)

	cases := []struct {
		name    string
		swap    string
		wantRe  string
		wantHit bool
	}{
		{
			name: "entrypoint dropped",
			swap: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
USER app
EXPOSE 8080
LABEL foo=bar
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
CMD ["python", "-m", "app"]
`,
			wantRe:  "ENTRYPOINT",
			wantHit: true,
		},
		{
			name: "entrypoint changed",
			swap: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
ENTRYPOINT ["/other.sh"]
USER app
EXPOSE 8080
LABEL foo=bar
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
CMD ["python", "-m", "app"]
`,
			wantRe:  "ENTRYPOINT",
			wantHit: true,
		},
		{
			name: "user changed",
			swap: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
ENTRYPOINT ["/entry.sh"]
USER root
EXPOSE 8080
LABEL foo=bar
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
CMD ["python", "-m", "app"]
`,
			wantRe:  "USER",
			wantHit: true,
		},
		{
			name: "expose changed",
			swap: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
ENTRYPOINT ["/entry.sh"]
USER app
EXPOSE 9090
LABEL foo=bar
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
CMD ["python", "-m", "app"]
`,
			wantRe:  "EXPOSE",
			wantHit: true,
		},
		{
			name: "label changed",
			swap: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
ENTRYPOINT ["/entry.sh"]
USER app
EXPOSE 8080
LABEL foo=baz
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
CMD ["python", "-m", "app"]
`,
			wantRe:  "LABEL",
			wantHit: true,
		},
		{
			name: "healthcheck changed",
			swap: `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
ENTRYPOINT ["/entry.sh"]
USER app
EXPOSE 8080
LABEL foo=bar
HEALTHCHECK CMD true
CMD ["python", "-m", "app"]
`,
			wantRe:  "HEALTHCHECK",
			wantHit: true,
		},
		{
			name:    "happy: identical runtime → no blocking",
			swap:    baseOrig,
			wantHit: false,
		},
	}

	o := &uvOverCondaObjective{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			proposed := mustParse(t, tc.swap)
			blocking := o.ValidateProposal(nil, orig, proposed)
			if !tc.wantHit && len(blocking) > 0 {
				t.Errorf("expected no blocking, got %v", blocking)
				return
			}
			if tc.wantHit {
				found := false
				for _, b := range blocking {
					if b.Rule == "runtime" && strings.Contains(b.Message, tc.wantRe) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected runtime block mentioning %q, got %v", tc.wantRe, blocking)
				}
			}
		})
	}
}

func TestUVOverCondaObjective_ValidateProposal_AddedRuntime(t *testing.T) {
	t.Parallel()

	orig := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
CMD ["python"]
`)
	proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENTRYPOINT ["/entry.sh"]
USER app
EXPOSE 8080
LABEL foo=bar
HEALTHCHECK CMD curl -f http://localhost/ || exit 1
CMD ["python"]
`)
	blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, orig, proposed)
	if len(blocking) == 0 {
		t.Fatal("expected blocking for added runtime instructions")
	}
}

func TestUVOverCondaObjective_ValidatePatch(t *testing.T) {
	t.Parallel()

	o := &uvOverCondaObjective{}
	if got := o.ValidatePatch(nil, patchutil.Meta{}); got != nil {
		t.Errorf("ValidatePatch = %v, want nil", got)
	}
}

// TestPreferUVOverCondaRule_CheckStage_PromotesPastFileLevelLocation is a
// regression test for a bug where, if the first qualifying conda install
// was on a RUN whose Location() was empty (file-level), the whole stage
// violation was dropped even though later RUNs had valid locations.
// Now the rule promotes past file-level-only RUNs and anchors on the
// first RUN with a real source range.
func TestPreferUVOverCondaRule_CheckStage_PromotesPastFileLevelLocation(t *testing.T) {
	t.Parallel()

	// Seed a Dockerfile whose second RUN has a real location via the parser.
	content := `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
RUN conda install -y numpy
CMD ["python"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	stageFacts := input.Facts.Stage(0)
	if stageFacts == nil || len(stageFacts.Runs) != 1 {
		t.Fatalf("expected 1 RUN in stage, got %d", len(stageFacts.Runs))
	}

	// Prepend a synthetic RUN whose underlying RunCommand has no location.
	// Facts assembled through the parser always carry location ranges, so
	// the only way to model a file-level-only RUN is to splice one in.
	fileLevelRun := &instructions.RunCommand{
		ShellDependantCmdLine: instructions.ShellDependantCmdLine{
			CmdLine: []string{"conda install -y torch"},
		},
	}
	if loc := fileLevelRun.Location(); len(loc) != 0 {
		t.Fatalf("expected synthetic RUN to have empty Location, got %v", loc)
	}
	syntheticRunFacts := &facts.RunFacts{
		Run: fileLevelRun,
		InstallCommands: []shell.InstallCommand{{
			Manager:    "conda",
			Subcommand: "install",
			Packages: []shell.PackageArg{
				{Normalized: "torch"},
			},
		}},
	}
	stageFacts.Runs = append([]*facts.RunFacts{syntheticRunFacts}, stageFacts.Runs...)

	violations := NewPreferUVOverCondaRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation (rule should promote past file-level RUN), got %d", len(violations))
	}
	if violations[0].Location.IsFileLevel() {
		t.Errorf("violation location is file-level; expected anchor on the second RUN")
	}
	// Signals from both RUNs should be present.
	req, ok := violations[0].SuggestedFix.ResolverData.(*autofixdata.ObjectiveRequest)
	if !ok {
		t.Fatalf("SuggestedFix.ResolverData is %T, want *autofixdata.ObjectiveRequest", violations[0].SuggestedFix.ResolverData)
	}
	if len(req.Signals) != 2 {
		t.Errorf("expected both RUN signals to be preserved, got %d", len(req.Signals))
	}
}

func TestStageGPUOriented_NilInputs(t *testing.T) {
	t.Parallel()

	if stageGPUOriented(nil, nil) {
		t.Error("stageGPUOriented(nil, nil) = true, want false")
	}
}

func TestHasHeavyCondaEnvWorkflow_NilStage(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", "FROM nvidia/cuda:12.1.0-devel-ubuntu22.04\nRUN echo ok\n")
	if hasHeavyCondaEnvWorkflow(input.Facts) {
		t.Error("hasHeavyCondaEnvWorkflow with no env signals = true, want false")
	}
}

func TestUVOverCondaTargetFile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		req  *autofixdata.ObjectiveRequest
		path string
		want string
	}{
		{"request file wins", &autofixdata.ObjectiveRequest{File: "sub/Dockerfile.dev"}, "other", "Dockerfile.dev"},
		{"fallback to path when request nil", nil, "dir/Dockerfile", "Dockerfile"},
		{"fallback when request file empty", &autofixdata.ObjectiveRequest{File: "   "}, "dir/Dockerfile.prod", "Dockerfile.prod"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := uvOverCondaTargetFile(tc.req, tc.path); got != tc.want {
				t.Errorf("uvOverCondaTargetFile = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunScriptText_Heredoc(t *testing.T) {
	t.Parallel()

	heredocRun := &instructions.RunCommand{
		ShellDependantCmdLine: instructions.ShellDependantCmdLine{
			Files: []instructions.ShellInlineFile{
				{Data: "conda install -y torch\n"},
			},
		},
	}
	got := runScriptText(heredocRun)
	if !strings.Contains(got, "conda install") {
		t.Errorf("runScriptText(heredoc) did not return heredoc body, got %q", got)
	}
}

// --- helpers ---

func mustParse(t *testing.T, content string) *dockerfile.ParseResult {
	t.Helper()
	pr, err := dockerfile.Parse(bytes.NewReader([]byte(content)), nil)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, content)
	}
	return pr
}

func contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}

func containsRule(issues []autofixdata.BlockingIssue, rule string) bool {
	for _, i := range issues {
		if i.Rule == rule {
			return true
		}
	}
	return false
}

func issueMessages(issues []autofixdata.BlockingIssue) []string {
	msgs := make([]string, 0, len(issues))
	for _, i := range issues {
		msgs = append(msgs, i.Message)
	}
	return msgs
}

// Ensure runScriptText covers both shell and exec form RUNs (for coverage).
func TestRunScriptText(t *testing.T) {
	t.Parallel()

	if got := runScriptText(nil); got != "" {
		t.Errorf("runScriptText(nil) = %q, want empty", got)
	}

	shellRun := &instructions.RunCommand{
		ShellDependantCmdLine: instructions.ShellDependantCmdLine{
			CmdLine: []string{"conda install -y torch"},
		},
	}
	if !strings.Contains(runScriptText(shellRun), "conda install") {
		t.Errorf("runScriptText(shell) did not include script, got %q", runScriptText(shellRun))
	}
}
