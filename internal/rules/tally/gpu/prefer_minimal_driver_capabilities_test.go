package gpu

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferMinimalDriverCapabilitiesRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewPreferMinimalDriverCapabilitiesRule().Metadata())
}

func TestPreferMinimalDriverCapabilitiesRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferMinimalDriverCapabilitiesRule(), []testutil.RuleTestCase{
		// ── Violations ──

		{
			Name: "all on nvidia/cuda base",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
`,
			WantViolations: 1,
			WantCodes:      []string{PreferMinimalDriverCapabilitiesRuleCode},
			WantMessages:   []string{"NVIDIA_DRIVER_CAPABILITIES=all exposes more driver libraries"},
		},
		{
			Name: "all on non-CUDA base still fires",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
`,
			WantViolations: 1,
			WantMessages:   []string{"NVIDIA_DRIVER_CAPABILITIES=all"},
		},
		{
			Name: "ALL uppercase",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=ALL
`,
			WantViolations: 1,
		},
		{
			Name: "quoted all",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES="all"
`,
			WantViolations: 1,
		},
		{
			Name: "mixed case All",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=All
`,
			WantViolations: 1,
		},
		{
			Name: "multi-key ENV with flagged key",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all CUDA_HOME=/usr/local/cuda
`,
			WantViolations: 1,
			WantMessages:   []string{"NVIDIA_DRIVER_CAPABILITIES=all"},
		},

		// ── No-fire cases ──

		{
			Name: "compute,utility is fine",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
`,
			WantViolations: 0,
		},
		{
			Name: "compute only",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=compute
`,
			WantViolations: 0,
		},
		{
			Name: "graphics,compute,utility",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=graphics,compute,utility
`,
			WantViolations: 0,
		},
		{
			Name: "empty value",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=
`,
			WantViolations: 0,
		},
		{
			Name: "variable reference with dollar-brace",
			Content: `FROM ubuntu:22.04
ARG CAPS=all
ENV NVIDIA_DRIVER_CAPABILITIES=${CAPS}
`,
			WantViolations: 0,
		},
		{
			Name: "variable reference with dollar",
			Content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=$CAPS
`,
			WantViolations: 0,
		},
		{
			Name: "no ENV instructions",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
RUN echo hello
`,
			WantViolations: 0,
		},
		{
			Name: "different NVIDIA ENV key not flagged",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_VISIBLE_DEVICES=all
`,
			WantViolations: 0,
		},

		// ── Multi-stage ──

		{
			Name: "multi-stage fires only in offending stage",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS base
ENV NVIDIA_DRIVER_CAPABILITIES=all

FROM ubuntu:22.04 AS app
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
`,
			WantViolations: 1,
		},
		{
			Name: "multiple offending stages",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04 AS base
ENV NVIDIA_DRIVER_CAPABILITIES=all

FROM ubuntu:22.04 AS app
ENV NVIDIA_DRIVER_CAPABILITIES=all
`,
			WantViolations: 2,
		},
		{
			Name: "overridden all then compute,utility no fire",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
`,
			WantViolations: 0,
		},
		{
			Name: "overridden compute,utility then all fires",
			Content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
ENV NVIDIA_DRIVER_CAPABILITIES=all
`,
			WantViolations: 1,
		},
	})
}

func TestPreferMinimalDriverCapabilitiesRule_FixSafety(t *testing.T) {
	t.Parallel()

	rule := NewPreferMinimalDriverCapabilitiesRule()

	tests := []struct {
		name       string
		content    string
		wantSafety rules.FixSafety
		wantFix    bool
	}{
		{
			name: "FixSuggestion for all",
			content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
`,
			wantSafety: rules.FixSuggestion,
			wantFix:    true,
		},
		{
			name: "FixSuggestion for all on non-CUDA base",
			content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
`,
			wantSafety: rules.FixSuggestion,
			wantFix:    true,
		},
		{
			name: "no fix for non-all value",
			content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
`,
			wantFix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			violations := rule.Check(input)
			if !tt.wantFix {
				if len(violations) > 0 && violations[0].SuggestedFix != nil {
					t.Errorf("expected no fix, got one")
				}
				return
			}
			if len(violations) == 0 {
				t.Fatal("expected violation")
			}
			fix := violations[0].SuggestedFix
			if fix == nil {
				t.Fatal("expected suggested fix")
			}
			if fix.Safety != tt.wantSafety {
				t.Errorf("safety = %v, want %v", fix.Safety, tt.wantSafety)
			}
			if !fix.IsPreferred {
				t.Error("expected IsPreferred = true")
			}
		})
	}
}

func TestPreferMinimalDriverCapabilitiesRule_FixEdit(t *testing.T) {
	t.Parallel()

	rule := NewPreferMinimalDriverCapabilitiesRule()

	tests := []struct {
		name        string
		content     string
		wantNewText string
	}{
		{
			name: "single-key replacement",
			content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
`,
			wantNewText: "ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility\n",
		},
		{
			name: "multi-key preserves other keys",
			content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all CUDA_HOME=/usr/local/cuda
`,
			wantNewText: "ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility CUDA_HOME=/usr/local/cuda\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			violations := rule.Check(input)
			if len(violations) == 0 {
				t.Fatal("expected violation")
			}
			fix := violations[0].SuggestedFix
			if fix == nil {
				t.Fatal("expected suggested fix")
			}
			if len(fix.Edits) == 0 {
				t.Fatal("expected at least one edit")
			}
			if fix.Edits[0].NewText != tt.wantNewText {
				t.Errorf("NewText = %q, want %q", fix.Edits[0].NewText, tt.wantNewText)
			}
		})
	}
}

func TestPreferMinimalDriverCapabilitiesRule_CheckFallback(t *testing.T) {
	t.Parallel()

	rule := NewPreferMinimalDriverCapabilitiesRule()

	tests := []struct {
		name           string
		content        string
		wantViolations int
	}{
		{
			name: "fires on all without facts",
			content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
`,
			wantViolations: 1,
		},
		{
			name: "no fire on compute,utility without facts",
			content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
`,
			wantViolations: 0,
		},
		{
			name: "no fire on variable ref without facts",
			content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=$CAPS
`,
			wantViolations: 0,
		},
		{
			name: "multi-key fires without facts",
			content: `FROM ubuntu:22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all CUDA_HOME=/usr/local/cuda
`,
			wantViolations: 1,
		},
		{
			name: "overridden all then compute,utility no fire without facts",
			content: `FROM nvidia/cuda:12.2.0-runtime-ubuntu22.04
ENV NVIDIA_DRIVER_CAPABILITIES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
`,
			wantViolations: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Build input without facts to exercise the fallback path.
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			input.Facts = nil

			violations := rule.Check(input)
			if len(violations) != tt.wantViolations {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantViolations)
				for i, v := range violations {
					t.Logf("  [%d] %s: %s", i, v.RuleCode, v.Message)
				}
			}
		})
	}
}

func TestIsDriverCapabilitiesAll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  bool
	}{
		{"all", true},
		{"ALL", true},
		{"All", true},
		{" all ", true},
		{"compute,utility", false},
		{"compute", false},
		{"", false},
		{"$CAPS", false},
		{"${CAPS}", false},
		{"none", false},
		{"graphics,compute,utility", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			if got := isDriverCapabilitiesAll(tt.value); got != tt.want {
				t.Errorf("isDriverCapabilitiesAll(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
