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
	// Both should trigger since neither has an absolute WORKDIR before it
	if len(violations) != 2 {
		t.Errorf("expected 2 violations, got %d", len(violations))
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

	// Two chained relative WORKDIRs: app then src
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
	// First: "app" against "/opt" → "/opt/app"
	// Second: "src" against "/opt/app" → "/opt/app/src"
	if len(edits) != 2 {
		t.Fatalf("got %d edits, want 2; edits: %v", len(edits), edits)
	}
	if edits[0] != "WORKDIR /opt/app" {
		t.Errorf("edit[0] = %q, want %q", edits[0], "WORKDIR /opt/app")
	}
	if edits[1] != "WORKDIR /opt/app/src" {
		t.Errorf("edit[1] = %q, want %q", edits[1], "WORKDIR /opt/app/src")
	}
}

func makeWorkdirHandler(t *testing.T, dockerfile string) *workdirRelPathHandler {
	t.Helper()
	r := NewWorkdirRelativePathRule()
	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)
	meta := r.Metadata()
	sem := testutil.GetSemantic(t, input)

	stagesWithViolations := make(map[int]bool)
	for stageIdx, stage := range input.Stages {
		isWindows := false
		if info := sem.StageInfo(stageIdx); info != nil {
			isWindows = info.IsWindows()
		}
		workdirSet := false
		for _, cmd := range stage.Commands {
			workdir, ok := cmd.(*instructions.WorkdirCommand)
			if !ok {
				continue
			}
			if isAbsPath(workdir.Path, isWindows) {
				workdirSet = true
			} else if !workdirSet {
				stagesWithViolations[stageIdx] = true
			}
		}
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

func TestIsAbsPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path      string
		isWindows bool
		want      bool
	}{
		// Linux paths
		{"/app", false, true},
		{"/", false, true},
		{"app", false, false},
		{"./app", false, false},
		{"../app", false, false},
		{"c:/build", false, false}, // drive letter is NOT absolute on Linux

		// Windows paths
		{"C:\\app", true, true},
		{"C:/app", true, true},
		{"c:/build", true, true}, // drive letter IS absolute on Windows
		{"/app", true, true},     // Forward slash is valid on Windows
		{"\\app", true, true},    // Backslash is valid on Windows
		{"app", true, false},
		{".\\app", true, false},
	}

	for _, tc := range tests {
		got := isAbsPath(tc.path, tc.isWindows)
		if got != tc.want {
			t.Errorf("isAbsPath(%q, isWindows=%v) = %v, want %v", tc.path, tc.isWindows, got, tc.want)
		}
	}
}
