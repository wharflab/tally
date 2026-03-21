package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestWorkdirRelativePathRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewWorkdirRelativePathRule().Metadata())
}

func TestWorkdirRelativePathRule_Check_RelativeWithoutAbsolute(t *testing.T) {
	t.Parallel()
	r := NewWorkdirRelativePathRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.WorkdirCommand{Path: "app"},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if violations[0].RuleCode != "buildkit/WorkdirRelativePath" {
		t.Errorf("expected code %q, got %q", "buildkit/WorkdirRelativePath", violations[0].RuleCode)
	}
}

func TestWorkdirRelativePathRule_Check_AbsoluteFirst(t *testing.T) {
	t.Parallel()
	r := NewWorkdirRelativePathRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.WorkdirCommand{Path: "/app"},
					&instructions.WorkdirCommand{Path: "src"}, // This is fine after absolute
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %d", len(violations))
	}
}

func TestWorkdirRelativePathRule_Check_OnlyAbsolute(t *testing.T) {
	t.Parallel()
	r := NewWorkdirRelativePathRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.WorkdirCommand{Path: "/app"},
				},
			},
		},
	}

	violations := r.Check(input)
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %d", len(violations))
	}
}

func TestWorkdirRelativePathRule_Check_MultipleRelativeBeforeAbsolute(t *testing.T) {
	t.Parallel()
	r := NewWorkdirRelativePathRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.WorkdirCommand{Path: "app"},
					&instructions.WorkdirCommand{Path: "src"},
				},
			},
		},
	}

	violations := r.Check(input)
	// Upstream BuildKit only warns on the first relative WORKDIR per stage;
	// once workdirSet is flipped, subsequent relative WORKDIRs are silent.
	if len(violations) != 1 {
		t.Errorf("expected 1 violation (first relative only), got %d", len(violations))
	}
}

func TestWorkdirRelativePathRule_Check_VariablePath(t *testing.T) {
	t.Parallel()
	r := NewWorkdirRelativePathRule()

	tests := []struct {
		name           string
		path           string
		wantViolations int
	}{
		{"bare variable", "${FUNCTION_DIR}", 0},
		{"variable with suffix", "${SOME_FOO}/the/suffix", 0},
		{"dollar without braces", "$APP_DIR", 0},
		{"relative literal", "app", 1},
		{"absolute with variable", "/app/${SUBDIR}", 0}, // absolute path
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := rules.LintInput{
				File: "Dockerfile",
				Stages: []instructions.Stage{
					{
						Commands: []instructions.Command{
							&instructions.WorkdirCommand{Path: tt.path},
						},
					},
				},
			}
			violations := r.Check(input)
			if len(violations) != tt.wantViolations {
				t.Errorf("path %q: got %d violations, want %d", tt.path, len(violations), tt.wantViolations)
			}
		})
	}
}

func TestWorkdirRelativePathRule_Check_MultipleStages(t *testing.T) {
	t.Parallel()
	r := NewWorkdirRelativePathRule()

	input := rules.LintInput{
		File: "Dockerfile",
		Stages: []instructions.Stage{
			{
				Commands: []instructions.Command{
					&instructions.WorkdirCommand{Path: "/app"},
				},
			},
			{
				// New stage, workdirSet resets
				Commands: []instructions.Command{
					&instructions.WorkdirCommand{Path: "src"},
				},
			},
		},
	}

	violations := r.Check(input)
	// Second stage has relative without absolute
	if len(violations) != 1 {
		t.Errorf("expected 1 violation, got %d", len(violations))
	}
}

// TestWorkdirRelativePathRule_WindowsDriveLetter verifies that c:/build is
// recognized as absolute on Windows stages via the semantic model.
func TestWorkdirRelativePathRule_WindowsDriveLetter(t *testing.T) {
	t.Parallel()
	r := NewWorkdirRelativePathRule()

	// With semantic model detecting Windows, c:/build is absolute → no violation
	testutil.RunRuleTests(t, r, []testutil.RuleTestCase{
		{
			Name: "Windows drive letter is absolute",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
WORKDIR c:/build
`,
			WantViolations: 0,
		},
		{
			Name: "Linux drive letter is relative",
			Content: `FROM alpine:3.20
WORKDIR c:/build
`,
			WantViolations: 1,
		},
	})
}

func TestWorkdirRelativePathRule_Check_InheritedFromParentStage(t *testing.T) {
	t.Parallel()

	// Parent stage sets absolute WORKDIR; child inherits it via FROM.
	// No violation should fire in the child stage for a relative WORKDIR.
	testutil.RunRuleTests(t, NewWorkdirRelativePathRule(), []testutil.RuleTestCase{
		{
			Name: "child inherits workdirSet from parent",
			Content: `FROM debian:bookworm AS base
WORKDIR /app
FROM base
WORKDIR src
`,
			WantViolations: 0,
		},
		{
			Name: "deep inheritance chain",
			Content: `FROM debian:bookworm AS base
WORKDIR /app
FROM base AS middle
RUN echo hi
FROM middle
WORKDIR sub
`,
			WantViolations: 0,
		},
		{
			Name: "parent has relative WORKDIR only — child still warns",
			Content: `FROM debian:bookworm AS base
WORKDIR app
FROM base
WORKDIR sub
`,
			// base triggers on "app"; child inherits workdirSet=true so no violation.
			WantViolations: 1,
		},
		{
			Name: "sibling stage does not inherit",
			Content: `FROM debian:bookworm AS base
WORKDIR /app
FROM debian:bookworm
WORKDIR src
`,
			// Second stage is FROM external image, not FROM base — no inheritance.
			WantViolations: 1,
		},
	})
}

func TestWorkdirRelativePathRule_Check_FixSuggestion(t *testing.T) {
	t.Parallel()

	input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nWORKDIR app\n")
	r := NewWorkdirRelativePathRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix, got nil")
	}
	if fix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", fix.Safety)
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("got %d edits, want 1", len(fix.Edits))
	}
	// Without registry data, resolves against "/" → "/app"
	if want := "WORKDIR /app"; fix.Edits[0].NewText != want {
		t.Errorf("NewText = %q, want %q", fix.Edits[0].NewText, want)
	}
}

func TestWorkdirRelativePathRule_PlanAsync(t *testing.T) {
	t.Parallel()

	t.Run("plans checks for stages with violations", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nWORKDIR app\n")
		r := NewWorkdirRelativePathRule()
		requests := r.PlanAsync(input)
		if len(requests) == 0 {
			t.Fatal("expected at least one async request")
		}
		if requests[0].RuleCode != rules.BuildKitRulePrefix+"WorkdirRelativePath" {
			t.Errorf("RuleCode = %q, want buildkit/WorkdirRelativePath", requests[0].RuleCode)
		}
	})

	t.Run("no plans when no violations", func(t *testing.T) {
		t.Parallel()
		input := testutil.MakeLintInput(t, "Dockerfile", "FROM alpine:3.18\nWORKDIR /app\n")
		r := NewWorkdirRelativePathRule()
		requests := r.PlanAsync(input)
		if len(requests) != 0 {
			t.Errorf("expected no async requests, got %d", len(requests))
		}
	})
}

func TestWorkdirRelativePathRule_Handler_BaseHasWorkingDir(t *testing.T) {
	t.Parallel()

	h := makeWorkdirHandler(t, "FROM alpine:3.18\nWORKDIR app\n")
	result := h.OnSuccess(&registry.ImageConfig{WorkingDir: "/opt"})
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	var hasCompleted, hasViolation bool
	for _, r := range result {
		switch v := r.(type) {
		case async.CompletedCheck:
			if v.StageIndex == 0 {
				hasCompleted = true
			}
		case rules.Violation:
			hasViolation = true
			if v.SuggestedFix == nil {
				t.Fatal("refined violation has no SuggestedFix")
			}
			if v.SuggestedFix.Safety != rules.FixSafe {
				t.Errorf("Safety = %v, want FixSafe", v.SuggestedFix.Safety)
			}
			// "app" resolved against "/opt" → "/opt/app"
			if want := "WORKDIR /opt/app"; v.SuggestedFix.Edits[0].NewText != want {
				t.Errorf("NewText = %q, want %q", v.SuggestedFix.Edits[0].NewText, want)
			}
		}
	}
	if !hasCompleted {
		t.Error("expected CompletedCheck for stage 0")
	}
	if !hasViolation {
		t.Error("expected refined violation")
	}
}

func TestWorkdirRelativePathRule_Handler_BaseWorkingDirWithNewline(t *testing.T) {
	t.Parallel()

	h := makeWorkdirHandler(t, "FROM alpine:3.18\nWORKDIR app\n")
	// Malicious WorkingDir with newline — handler should bail entirely.
	result := h.OnSuccess(&registry.ImageConfig{WorkingDir: "/opt\nRUN malicious"})
	if result != nil {
		t.Errorf("expected nil result for path with newline injection, got %d items", len(result))
	}
}

func TestWorkdirRelativePathRule_Handler_BaseNoWorkingDir(t *testing.T) {
	t.Parallel()

	h := makeWorkdirHandler(t, "FROM alpine:3.18\nWORKDIR app\n")
	result := h.OnSuccess(&registry.ImageConfig{WorkingDir: ""})
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Even with no base WORKDIR, the handler resolves against "/" and
	// upgrades the fix to FixSafe (registry confirmed).
	var hasViolation bool
	for _, r := range result {
		if v, ok := r.(rules.Violation); ok {
			hasViolation = true
			if v.SuggestedFix == nil || len(v.SuggestedFix.Edits) == 0 {
				t.Fatal("missing fix edits")
			}
			// "app" resolved against "/" → "/app"
			if want := "WORKDIR /app"; v.SuggestedFix.Edits[0].NewText != want {
				t.Errorf("NewText = %q, want %q", v.SuggestedFix.Edits[0].NewText, want)
			}
			if v.SuggestedFix.Safety != rules.FixSafe {
				t.Errorf("Safety = %v, want FixSafe", v.SuggestedFix.Safety)
			}
		}
	}
	if !hasViolation {
		t.Error("expected refined violation")
	}
}

func TestWorkdirRelativePathRule_Handler_ChainedRelative(t *testing.T) {
	t.Parallel()

	// Two chained relative WORKDIRs: app then src.
	// Upstream BuildKit only warns on the first relative WORKDIR per stage,
	// so the handler should emit only one refined violation.
	h := makeWorkdirHandler(t, "FROM alpine:3.18\nWORKDIR app\nWORKDIR src\n")
	result := h.OnSuccess(&registry.ImageConfig{WorkingDir: "/opt"})
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	var edits []string
	for _, r := range result {
		if v, ok := r.(rules.Violation); ok {
			if v.SuggestedFix != nil && len(v.SuggestedFix.Edits) > 0 {
				edits = append(edits, v.SuggestedFix.Edits[0].NewText)
			}
		}
	}
	// Only the first relative WORKDIR ("app" against "/opt") triggers.
	if len(edits) != 1 {
		t.Fatalf("got %d edits, want 1; edits: %v", len(edits), edits)
	}
	if edits[0] != "WORKDIR /opt/app" {
		t.Errorf("edit[0] = %q, want %q", edits[0], "WORKDIR /opt/app")
	}
}

func makeWorkdirHandler(t *testing.T, dockerfile string) *workdirRelPathHandler {
	t.Helper()
	r := NewWorkdirRelativePathRule()
	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)
	meta := r.Metadata()
	sem := testutil.GetSemantic(t, input)

	violations := findRelativeWorkdirViolations(sem, input.Stages, input.File, meta)
	stagesWithViolations := make(map[int]bool, len(violations))
	for _, v := range violations {
		stagesWithViolations[v.StageIndex] = true
	}

	return &workdirRelPathHandler{
		meta:                 meta,
		file:                 input.File,
		stageIdx:             0,
		semantic:             sem,
		stages:               input.Stages,
		stagesWithViolations: stagesWithViolations,
	}
}
