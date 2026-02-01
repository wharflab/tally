package hadolint

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3003Rule_Check(t *testing.T) {
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Cases from original Hadolint spec
		{
			name:       "ok using WORKDIR",
			dockerfile: "FROM ubuntu\nWORKDIR /opt",
			wantCount:  0,
		},
		{
			name:       "not ok using cd",
			dockerfile: "FROM ubuntu\nRUN cd /opt",
			wantCount:  1,
		},

		// Additional test cases
		{
			name:       "cd with chained command",
			dockerfile: "FROM ubuntu\nRUN cd /app && make build",
			wantCount:  1,
		},
		{
			name:       "cd at end of chain (useless but still flagged)",
			dockerfile: "FROM ubuntu\nRUN make build && cd /app",
			wantCount:  1,
		},
		{
			name:       "multiple cd commands",
			dockerfile: "FROM ubuntu\nRUN cd /app && cd /other",
			wantCount:  1, // One violation per RUN, not per cd
		},
		{
			name:       "no cd command",
			dockerfile: "FROM ubuntu\nRUN echo hello",
			wantCount:  0,
		},
		{
			name:       "cd in subshell",
			dockerfile: "FROM ubuntu\nRUN (cd /app && make)",
			wantCount:  1,
		},
		{
			name:       "cd with environment variable",
			dockerfile: "FROM ubuntu\nRUN cd $HOME",
			wantCount:  1,
		},
		{
			name:       "cd with quoted path",
			dockerfile: `FROM ubuntu` + "\n" + `RUN cd "/path with spaces"`,
			wantCount:  1,
		},
		{
			name:       "multiple RUN instructions with cd",
			dockerfile: "FROM ubuntu\nRUN cd /app\nRUN cd /other",
			wantCount:  2,
		},
		{
			name:       "WORKDIR followed by RUN without cd",
			dockerfile: "FROM ubuntu\nWORKDIR /app\nRUN make build",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewDL3003Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  violation: %s", v.Message)
				}
			}
		})
	}
}

func TestDL3003Rule_AutoFix(t *testing.T) {
	tests := []struct {
		name            string
		dockerfile      string
		wantFix         bool
		wantFixContains string
	}{
		{
			name:            "standalone cd gets WORKDIR fix",
			dockerfile:      "FROM ubuntu\nRUN cd /opt",
			wantFix:         true,
			wantFixContains: "WORKDIR /opt",
		},
		{
			name:            "cd with chain gets split fix",
			dockerfile:      "FROM ubuntu\nRUN cd /app && make build",
			wantFix:         true,
			wantFixContains: "WORKDIR /app",
		},
		{
			name:       "cd at end of chain - no fix",
			dockerfile: "FROM ubuntu\nRUN make build && cd /app",
			wantFix:    false,
		},
		{
			name:       "cd with variable - no fix (can't determine path)",
			dockerfile: "FROM ubuntu\nRUN cd $HOME",
			wantFix:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewDL3003Rule()
			violations := r.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			hasFix := v.SuggestedFix != nil

			if hasFix != tt.wantFix {
				t.Errorf("hasFix = %v, want %v", hasFix, tt.wantFix)
			}

			if tt.wantFix && v.SuggestedFix != nil {
				if len(v.SuggestedFix.Edits) == 0 {
					t.Error("fix has no edits")
				} else {
					newText := v.SuggestedFix.Edits[0].NewText
					if tt.wantFixContains != "" && !strings.Contains(newText, tt.wantFixContains) {
						t.Errorf("fix NewText = %q, want to contain %q", newText, tt.wantFixContains)
					}
				}
			}
		})
	}
}

// TestDL3003_FixLocationConsistency is a regression test ensuring that
// fix edit locations use the same line numbering as violation locations.
// Previously, violations used 1-based lines (BuildKit) while edits used 0-based.
func TestDL3003_FixLocationConsistency(t *testing.T) {
	input := testutil.MakeLintInput(t, "Dockerfile", "FROM ubuntu\nRUN cd /opt")
	r := NewDL3003Rule()
	violations := r.Check(input)

	if len(violations) == 0 {
		t.Fatal("expected at least one violation")
	}

	v := violations[0]
	if v.SuggestedFix == nil || len(v.SuggestedFix.Edits) == 0 {
		t.Fatal("expected SuggestedFix with edits")
	}

	// The violation is on line 2 (1-based: "RUN cd /opt")
	// The fix edit should also be on line 2
	violationLine := v.Location.Start.Line
	editLine := v.SuggestedFix.Edits[0].Location.Start.Line

	if violationLine != editLine {
		t.Errorf("line number mismatch: violation on line %d, edit on line %d (should be equal)",
			violationLine, editLine)
	}

	// Both should be 1-based (line 2, not line 1)
	if violationLine != 2 {
		t.Errorf("expected violation on line 2 (1-based), got %d", violationLine)
	}
}
