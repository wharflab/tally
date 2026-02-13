package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3027Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Original Hadolint test cases - should trigger
		{
			name: "apt install command",
			dockerfile: `FROM ubuntu
RUN apt install python`,
			wantCount: 1,
		},

		// Additional test cases for comprehensive coverage
		{
			name: "apt-get install should not trigger",
			dockerfile: `FROM ubuntu
RUN apt-get install python`,
			wantCount: 0,
		},
		{
			name: "apt-cache should not trigger",
			dockerfile: `FROM ubuntu
RUN apt-cache search python`,
			wantCount: 0,
		},
		{
			name: "apt update command",
			dockerfile: `FROM ubuntu
RUN apt update`,
			wantCount: 1,
		},
		{
			name: "apt upgrade command",
			dockerfile: `FROM ubuntu
RUN apt upgrade`,
			wantCount: 1,
		},
		{
			name: "apt with full path",
			dockerfile: `FROM ubuntu
RUN /usr/bin/apt install python`,
			wantCount: 1,
		},
		{
			name: "apt in command chain",
			dockerfile: `FROM ubuntu
RUN apt update && apt install python`,
			wantCount: 1, // Single violation with multiple edits for all apt commands
		},
		{
			name: "apt with sudo",
			dockerfile: `FROM ubuntu
RUN sudo apt install python`,
			// Note: sudo is intentionally not a transparent wrapper in our shell parser
			// so that DL3004 can detect it. In this case, only sudo is detected, not apt.
			// This is a design trade-off: detecting both commands would require sudo
			// to be in commandWrappers, but then DL3004 checking would need adjustment.
			wantCount: 0,
		},
		{
			name: "apt with env wrapper",
			dockerfile: `FROM ubuntu
RUN env DEBIAN_FRONTEND=noninteractive apt install python`,
			wantCount: 1,
		},
		{
			name: "apt in shell -c",
			dockerfile: `FROM ubuntu
RUN sh -c 'apt install python'`,
			wantCount: 1,
		},
		{
			name: "word 'apt' in string should not trigger",
			dockerfile: `FROM ubuntu
RUN echo "adapt to changes"`,
			wantCount: 0,
		},
		{
			name: "word 'apt' in package name should not trigger",
			dockerfile: `FROM ubuntu
RUN apt-get install aptitude`,
			wantCount: 0,
		},
		{
			name: "multiple RUN commands with apt",
			dockerfile: `FROM ubuntu
RUN apt update
RUN apt install python
RUN apt upgrade`,
			wantCount: 3,
		},
		{
			name: "apt in exec form",
			dockerfile: `FROM ubuntu
RUN ["apt", "install", "python"]`,
			wantCount: 1,
		},
		{
			name: "multi-stage with apt in one stage",
			dockerfile: `FROM ubuntu AS builder
RUN apt install python

FROM alpine
RUN apk add python`,
			wantCount: 1,
		},
		{
			name: "ONBUILD with apt",
			dockerfile: `FROM ubuntu
ONBUILD RUN apt install python`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)
			r := NewDL3027Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for i, v := range violations {
					t.Logf("violation %d: %s at %v", i+1, v.Message, v.Location)
				}
			}

			// Verify violation details for positive cases
			if tt.wantCount > 0 && len(violations) > 0 {
				v := violations[0]
				if v.RuleCode != "hadolint/DL3027" {
					t.Errorf("got rule code %q, want %q", v.RuleCode, "hadolint/DL3027")
				}
				if v.Message == "" {
					t.Error("violation message is empty")
				}
				if v.Detail == "" {
					t.Error("violation detail is empty")
				}
				if v.DocURL != "https://github.com/hadolint/hadolint/wiki/DL3027" {
					t.Errorf("got doc URL %q, want %q", v.DocURL, "https://github.com/hadolint/hadolint/wiki/DL3027")
				}
			}
		})
	}
}

func TestDL3027_MultipleApt(t *testing.T) {
	t.Parallel()
	input := testutil.MakeLintInput(t, "Dockerfile", "FROM ubuntu\nRUN apt update && apt install curl")
	r := NewDL3027Rule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if len(v.SuggestedFix.Edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(v.SuggestedFix.Edits))
	}

	// First edit should be at column 4 (first apt)
	if v.SuggestedFix.Edits[0].Location.Start.Column != 4 {
		t.Errorf("first edit startCol = %d, want 4", v.SuggestedFix.Edits[0].Location.Start.Column)
	}

	// Second edit should be at column 18 (second apt)
	if v.SuggestedFix.Edits[1].Location.Start.Column != 18 {
		t.Errorf("second edit startCol = %d, want 18", v.SuggestedFix.Edits[1].Location.Start.Column)
	}
}

func TestDL3027Rule_SuggestedFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		dockerfile      string
		wantReplacement string
		wantSafety      string
	}{
		{
			name: "apt install -> apt-get (safe)",
			dockerfile: `FROM ubuntu
RUN apt install curl`,
			wantReplacement: "apt-get",
			wantSafety:      "safe",
		},
		{
			name: "apt update -> apt-get (safe)",
			dockerfile: `FROM ubuntu
RUN apt update`,
			wantReplacement: "apt-get",
			wantSafety:      "safe",
		},
		{
			name: "apt search -> apt-cache (suggestion)",
			dockerfile: `FROM ubuntu
RUN apt search curl`,
			wantReplacement: "apt-cache",
			wantSafety:      "suggestion",
		},
		{
			name: "apt show -> apt-cache (suggestion)",
			dockerfile: `FROM ubuntu
RUN apt show curl`,
			wantReplacement: "apt-cache",
			wantSafety:      "suggestion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewDL3027Rule()
			violations := r.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			if v.SuggestedFix == nil {
				t.Fatal("SuggestedFix is nil")
			}

			if len(v.SuggestedFix.Edits) == 0 {
				t.Fatal("SuggestedFix has no edits")
			}

			edit := v.SuggestedFix.Edits[0]
			if edit.NewText != tt.wantReplacement {
				t.Errorf("got replacement %q, want %q", edit.NewText, tt.wantReplacement)
			}

			gotSafety := v.SuggestedFix.Safety.String()
			if gotSafety != tt.wantSafety {
				t.Errorf("got safety %q, want %q", gotSafety, tt.wantSafety)
			}
		})
	}
}

func TestDL3027Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3027Rule().Metadata())
}

// TestDL3027_FixLocationConsistency is a regression test ensuring that
// fix edit locations use the same line numbering as violation locations.
// Previously, violations used 1-based lines (BuildKit) while edits used 0-based.
func TestDL3027_FixLocationConsistency(t *testing.T) {
	t.Parallel()
	input := testutil.MakeLintInput(t, "Dockerfile", "FROM ubuntu\nRUN apt install curl")
	r := NewDL3027Rule()
	violations := r.Check(input)

	if len(violations) == 0 {
		t.Fatal("expected at least one violation")
	}

	v := violations[0]
	if v.SuggestedFix == nil || len(v.SuggestedFix.Edits) == 0 {
		t.Fatal("expected SuggestedFix with edits")
	}

	// The violation is on line 2 (1-based: "RUN apt install curl")
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

// TestDL3027_WhitespaceDrift verifies that auto-fix works correctly even with
// extra whitespace in the RUN command, since we parse the original source.
func TestDL3027_WhitespaceDrift(t *testing.T) {
	t.Parallel()
	// RUN with extra spaces - parsing original source handles this correctly
	dockerfile := "FROM ubuntu\nRUN    apt   install curl"

	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)
	r := NewDL3027Rule()
	violations := r.Check(input)

	if len(violations) == 0 {
		t.Fatal("expected violation")
	}

	v := violations[0]
	// With original source parsing, we should get correct edits
	if v.SuggestedFix == nil || len(v.SuggestedFix.Edits) == 0 {
		t.Fatal("expected edits for whitespace case")
	}

	// Verify the edit points to the correct position (column 7, after "RUN    ")
	edit := v.SuggestedFix.Edits[0]
	if edit.Location.Start.Column != 7 {
		t.Errorf("edit startCol = %d, want 7", edit.Location.Start.Column)
	}
}

// TestDL3027_MultiLineRUN verifies that multi-line RUN commands are detected
// AND auto-fixed correctly by parsing the original source with preserved positions.
func TestDL3027_MultiLineRUN(t *testing.T) {
	t.Parallel()
	// Multi-line RUN with backslash continuation - apt on different physical lines
	dockerfile := `FROM ubuntu
RUN apt-get update && \
    apt install python`

	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)
	r := NewDL3027Rule()
	violations := r.Check(input)

	// Should detect the violation
	if len(violations) == 0 {
		t.Fatal("expected violation for multi-line RUN with apt")
	}

	v := violations[0]

	// Violation should be reported
	if v.RuleCode != "hadolint/DL3027" {
		t.Errorf("expected rule code hadolint/DL3027, got %s", v.RuleCode)
	}

	// Auto-fix SHOULD be generated for multi-line RUN now
	// since we parse original source with correct positions
	if v.SuggestedFix == nil || len(v.SuggestedFix.Edits) == 0 {
		t.Fatal("expected edits for multi-line RUN")
	}

	// The edit should be on line 3 (the continuation line with "apt install")
	edit := v.SuggestedFix.Edits[0]
	if edit.Location.Start.Line != 3 {
		t.Errorf("edit line = %d, want 3", edit.Location.Start.Line)
	}
	// apt is at column 4 (after "    ")
	if edit.Location.Start.Column != 4 {
		t.Errorf("edit startCol = %d, want 4", edit.Location.Start.Column)
	}
}

// TestDL3027_OnbuildAutoFix verifies that DL3027 provides auto-fix for ONBUILD RUN commands.
func TestDL3027_OnbuildAutoFix(t *testing.T) {
	t.Parallel()
	input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", `FROM ubuntu
ONBUILD RUN apt install python`)
	r := NewDL3027Rule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	v := violations[0]

	// Violation should point to the ONBUILD line
	if v.Location.Start.Line != 2 {
		t.Errorf("violation line = %d, want 2", v.Location.Start.Line)
	}

	// Auto-fix should replace "apt" with "apt-get"
	if v.SuggestedFix == nil {
		t.Fatal("expected SuggestedFix for ONBUILD RUN")
	}
	if len(v.SuggestedFix.Edits) == 0 {
		t.Fatal("expected at least one edit")
	}
	edit := v.SuggestedFix.Edits[0]
	// "ONBUILD RUN apt install python" — "apt" starts at column 12
	if edit.Location.Start.Column != 12 {
		t.Errorf("edit startCol = %d, want 12", edit.Location.Start.Column)
	}
	if edit.Location.End.Column != 15 {
		t.Errorf("edit endCol = %d, want 15", edit.Location.End.Column)
	}
	if edit.NewText != "apt-get" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "apt-get")
	}
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit line = %d, want 2", edit.Location.Start.Line)
	}
}
