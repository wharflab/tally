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
	patchutil "github.com/wharflab/tally/internal/patch"
	"github.com/wharflab/tally/internal/rules"
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
	}
	for in, want := range cases {
		if got := normalizeCondaPackageName(in); got != want {
			t.Errorf("normalizeCondaPackageName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsCondaManager(t *testing.T) {
	t.Parallel()

	for _, m := range []string{"conda", "mamba", "micromamba"} {
		if !isCondaManager(m) {
			t.Errorf("isCondaManager(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"", "pip", "pip3", "uv", "apt", "apt-get"} {
		if isCondaManager(m) {
			t.Errorf("isCondaManager(%q) = true, want false", m)
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
		blocking := (&uvOverCondaObjective{}).ValidateProposal(orig, proposed)
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
		blocking := (&uvOverCondaObjective{}).ValidateProposal(orig, proposed)
		if !containsRule(blocking, "migration") {
			t.Errorf("expected migration blocking issue, got %v", blocking)
		}
	})

	t.Run("blocks when CMD is dropped", func(t *testing.T) {
		t.Parallel()
		proposed := mustParse(t, `FROM nvidia/cuda:12.1.0-runtime-ubuntu22.04
WORKDIR /app
ENV FOO=bar
RUN uv pip install --system numpy torch
`)
		blocking := (&uvOverCondaObjective{}).ValidateProposal(orig, proposed)
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
		blocking := (&uvOverCondaObjective{}).ValidateProposal(orig, proposed)
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
		blocking := (&uvOverCondaObjective{}).ValidateProposal(orig, proposed)
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
		blocking := (&uvOverCondaObjective{}).ValidateProposal(sysOrig, sysProposed)
		for _, b := range blocking {
			if b.Rule == "migration" {
				t.Errorf("unexpected migration block for system conda install: %v", b)
			}
		}
	})

	t.Run("handles nil parse results", func(t *testing.T) {
		t.Parallel()
		blocking := (&uvOverCondaObjective{}).ValidateProposal(nil, nil)
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
			blocking := o.ValidateProposal(orig, proposed)
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
	blocking := (&uvOverCondaObjective{}).ValidateProposal(orig, proposed)
	if len(blocking) == 0 {
		t.Fatal("expected blocking for added runtime instructions")
	}
}

func TestUVOverCondaObjective_ValidatePatch(t *testing.T) {
	t.Parallel()

	o := &uvOverCondaObjective{}
	if got := o.ValidatePatch(patchutil.Meta{}); got != nil {
		t.Errorf("ValidatePatch = %v, want nil", got)
	}
}

func TestFirstCondaPythonMLPackage(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"conda install -y torch numpy":          true,
		"conda install -y flash-attn":           true,
		"micromamba install -y xformers":        true,
		"conda install -y gcc cmake":            false,
		"conda install -y build-essential make": false,
	}
	for script, wantHit := range cases {
		got := firstCondaPythonMLPackage(strings.ToLower(script))
		if wantHit && got == "" {
			t.Errorf("firstCondaPythonMLPackage(%q) = %q, want non-empty", script, got)
		}
		if !wantHit && got != "" {
			t.Errorf("firstCondaPythonMLPackage(%q) = %q, want empty", script, got)
		}
	}
}

func TestCommandSegmentEnd(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want int
	}{
		{"no terminator", "install torch", len("install torch")},
		{"semicolon", "install torch; echo", len("install torch")},
		{"pipe", "install torch | grep x", len("install torch ")},
		{"ampersand", "install torch && echo", len("install torch ")},
		{"newline stays inside segment", "install \\\nfoo", len("install \\\nfoo")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := commandSegmentEnd(tc.in, 0); got != tc.want {
				t.Errorf("commandSegmentEnd(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsPkgNameByte(t *testing.T) {
	t.Parallel()

	yes := []byte{'a', 'z', '0', '9', '_', '.'}
	for _, b := range yes {
		if !isPkgNameByte(b) {
			t.Errorf("isPkgNameByte(%q) = false, want true", b)
		}
	}
	no := []byte{' ', '-', '+', '=', '"', 'A', 'Z'}
	for _, b := range no {
		if isPkgNameByte(b) {
			t.Errorf("isPkgNameByte(%q) = true, want false", b)
		}
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

func TestCondaFlagTakesValue(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"-c", "--channel", "-n", "--name", "-p", "--prefix", "-f", "--file"} {
		if !condaFlagTakesValue(flag) {
			t.Errorf("condaFlagTakesValue(%q) = false, want true", flag)
		}
	}
	for _, flag := range []string{"--yes", "-y", "--no-deps", "--channel=foo", ""} {
		if condaFlagTakesValue(flag) {
			t.Errorf("condaFlagTakesValue(%q) = true, want false", flag)
		}
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

func TestContainsAsToken(t *testing.T) {
	t.Parallel()

	if !containsAsToken("conda install -y torch", "torch") {
		t.Error("expected token match for torch")
	}
	// "pytorch-cuda" should not match bare "torch" because the boundary rule
	// excludes it (when preceded by alphanumeric characters).
	if containsAsToken("conda install pytorch-cuda", "torch") {
		t.Error("bare 'torch' should not match inside 'pytorch-cuda'")
	}
	if !containsAsToken("install xformers ", "xformers") {
		t.Error("expected token match for xformers")
	}
	// substring at very start
	if !containsAsToken("torch", "torch") {
		t.Error("expected match when substring is whole string")
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
