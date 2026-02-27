package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

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
