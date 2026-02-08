package buildkit

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
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

func TestIsAbsPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		os   string
		want bool
	}{
		// Linux paths
		{"/app", "linux", true},
		{"/", "linux", true},
		{"app", "linux", false},
		{"./app", "linux", false},
		{"../app", "linux", false},

		// Windows paths
		{"C:\\app", "windows", true},
		{"C:/app", "windows", true},
		{"/app", "windows", true},  // Forward slash is valid on Windows
		{"\\app", "windows", true}, // Backslash is valid on Windows
		{"app", "windows", false},
		{".\\app", "windows", false},
	}

	for _, tc := range tests {
		got := isAbsPath(tc.path, tc.os)
		if got != tc.want {
			t.Errorf("isAbsPath(%q, %q) = %v, want %v", tc.path, tc.os, got, tc.want)
		}
	}
}
