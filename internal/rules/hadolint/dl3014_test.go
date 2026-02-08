package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3014Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Original Hadolint test cases - should trigger (ruleCatches)
		{
			name: "apt-get install without -y",
			dockerfile: `FROM ubuntu
RUN apt-get install python`,
			wantCount: 1,
		},
		{
			name: "ONBUILD apt-get install without -y",
			dockerfile: `FROM ubuntu
ONBUILD RUN apt-get install python`,
			wantCount: 0, // ONBUILD not yet supported
		},

		// Insufficient Quiet Levels - should trigger
		{
			name: "apt-get install -q (single)",
			dockerfile: `FROM ubuntu
RUN apt-get install -q python`,
			wantCount: 1,
		},
		{
			name: "apt-get install --quiet (single)",
			dockerfile: `FROM ubuntu
RUN apt-get install --quiet python`,
			wantCount: 1,
		},

		// Original Hadolint test cases - should NOT trigger (ruleCatchesNot)
		{
			name: "apt-get install -yq",
			dockerfile: `FROM ubuntu
RUN apt-get install -yq python`,
			wantCount: 0,
		},
		{
			name: "apt-get install -y",
			dockerfile: `FROM ubuntu
RUN apt-get install -y python`,
			wantCount: 0,
		},
		{
			name: "apt-get -y install",
			dockerfile: `FROM ubuntu
RUN apt-get -y install python`,
			wantCount: 0,
		},
		{
			name: "apt-get --yes install",
			dockerfile: `FROM ubuntu
RUN apt-get --yes install python`,
			wantCount: 0,
		},
		{
			name: "apt-get --assume-yes install",
			dockerfile: `FROM ubuntu
RUN apt-get --assume-yes install python`,
			wantCount: 0,
		},
		{
			name: "apt-get install -qq",
			dockerfile: `FROM ubuntu
RUN apt-get install -qq python`,
			wantCount: 0,
		},
		{
			name: "apt-get install -q -q",
			dockerfile: `FROM ubuntu
RUN apt-get install -q -q python`,
			wantCount: 0,
		},
		{
			name: "apt-get install -q=2",
			dockerfile: `FROM ubuntu
RUN apt-get install -q=2 python`,
			wantCount: 0,
		},
		{
			name: "apt-get install --quiet --quiet",
			dockerfile: `FROM ubuntu
RUN apt-get install --quiet --quiet python`,
			wantCount: 0,
		},

		// Additional test cases
		{
			name: "apt-get update (no violation for non-install)",
			dockerfile: `FROM ubuntu
RUN apt-get update`,
			wantCount: 0,
		},
		{
			name: "apt-get remove (no violation for non-install)",
			dockerfile: `FROM ubuntu
RUN apt-get remove python`,
			wantCount: 0,
		},
		{
			name: "chained apt-get commands",
			dockerfile: `FROM ubuntu
RUN apt-get update && apt-get install python`,
			wantCount: 1,
		},
		{
			name: "chained apt-get with -y",
			dockerfile: `FROM ubuntu
RUN apt-get update && apt-get install -y python`,
			wantCount: 0,
		},
		{
			name: "multiple install commands without -y",
			dockerfile: `FROM ubuntu
RUN apt-get install python && apt-get install curl`,
			wantCount: 2,
		},
		{
			name: "with env wrapper",
			dockerfile: `FROM ubuntu
RUN env DEBIAN_FRONTEND=noninteractive apt-get install python`,
			wantCount: 1,
		},
		{
			name: "with env wrapper and -y",
			dockerfile: `FROM ubuntu
RUN env DEBIAN_FRONTEND=noninteractive apt-get install -y python`,
			wantCount: 0,
		},
		{
			name: "apt (not apt-get) should not trigger DL3014",
			dockerfile: `FROM ubuntu
RUN apt install python`,
			wantCount: 0, // DL3014 is specifically for apt-get
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewDL3014Rule()
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
				if v.RuleCode != "hadolint/DL3014" {
					t.Errorf("got rule code %q, want %q", v.RuleCode, "hadolint/DL3014")
				}
				if v.Message == "" {
					t.Error("violation message is empty")
				}
				if v.Detail == "" {
					t.Error("violation detail is empty")
				}
				if v.DocURL != "https://github.com/hadolint/hadolint/wiki/DL3014" {
					t.Errorf("got doc URL %q, want %q", v.DocURL, "https://github.com/hadolint/hadolint/wiki/DL3014")
				}
			}
		})
	}
}

// TestDL3014_AutoFix verifies that DL3014 provides auto-fix suggestions.
func TestDL3014_AutoFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		dockerfile    string
		wantFix       bool
		wantInsertCol int
		wantNewText   string
	}{
		{
			name: "simple apt-get install",
			dockerfile: `FROM ubuntu
RUN apt-get install python`,
			wantFix:       true,
			wantInsertCol: 19, // After "install"
			wantNewText:   " -y",
		},
		{
			name: "apt-get install with package",
			dockerfile: `FROM ubuntu
RUN apt-get install curl wget`,
			wantFix:       true,
			wantInsertCol: 19,
			wantNewText:   " -y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewDL3014Rule()
			violations := r.Check(input)

			if len(violations) == 0 {
				t.Fatal("expected at least one violation")
			}

			v := violations[0]
			if tt.wantFix {
				if v.SuggestedFix == nil {
					t.Fatal("expected SuggestedFix")
				}
				if len(v.SuggestedFix.Edits) == 0 {
					t.Fatal("expected at least one edit")
				}
				edit := v.SuggestedFix.Edits[0]
				if edit.Location.Start.Column != tt.wantInsertCol {
					t.Errorf("insert column = %d, want %d", edit.Location.Start.Column, tt.wantInsertCol)
				}
				if edit.NewText != tt.wantNewText {
					t.Errorf("NewText = %q, want %q", edit.NewText, tt.wantNewText)
				}
			}
		})
	}
}

// TestDL3014AndDL3027CombinedFixes verifies that DL3027 and DL3014 can both
// provide fixes on the same RUN command when it contains both "apt" and "apt-get install".
func TestDL3014AndDL3027CombinedFixes(t *testing.T) {
	t.Parallel()
	// This Dockerfile has both:
	// - apt update (triggers DL3027, wants to change apt -> apt-get)
	// - apt-get install (triggers DL3014, wants to add -y)
	dockerfile := `FROM ubuntu
RUN apt update && apt-get install python`

	input := testutil.MakeLintInput(t, "Dockerfile", dockerfile)

	dl3027 := NewDL3027Rule()
	dl3014 := NewDL3014Rule()

	dl3027Violations := dl3027.Check(input)
	dl3014Violations := dl3014.Check(input)

	// DL3027 should fire for "apt update"
	if len(dl3027Violations) != 1 {
		t.Fatalf("DL3027: expected 1 violation, got %d", len(dl3027Violations))
	}

	v27 := dl3027Violations[0]
	if v27.SuggestedFix == nil || len(v27.SuggestedFix.Edits) == 0 {
		t.Fatal("DL3027: expected SuggestedFix with edits")
	}

	// Edit should be at column 4 (where "apt" starts after "RUN ")
	edit27 := v27.SuggestedFix.Edits[0]
	if edit27.Location.Start.Column != 4 {
		t.Errorf("DL3027: edit column = %d, want 4", edit27.Location.Start.Column)
	}
	if edit27.NewText != "apt-get" {
		t.Errorf("DL3027: NewText = %q, want %q", edit27.NewText, "apt-get")
	}
	if edit27.Location.Start.Line != 2 {
		t.Error("DL3027: edit should be on line 2")
	}

	// DL3014 should fire for "apt-get install"
	if len(dl3014Violations) != 1 {
		t.Fatalf("DL3014: expected 1 violation, got %d", len(dl3014Violations))
	}

	v14 := dl3014Violations[0]
	if v14.SuggestedFix == nil || len(v14.SuggestedFix.Edits) == 0 {
		t.Fatal("DL3014: expected SuggestedFix with edits")
	}

	// Edit should be after "install" (column 33)
	edit14 := v14.SuggestedFix.Edits[0]
	if edit14.Location.Start.Column != 33 {
		t.Errorf("DL3014: edit column = %d, want 33", edit14.Location.Start.Column)
	}
	if edit14.NewText != " -y" {
		t.Errorf("DL3014: NewText = %q, want %q", edit14.NewText, " -y")
	}
	if edit14.Location.Start.Line != 2 {
		t.Error("DL3014: edit should be on line 2")
	}
}

// TestDL3014AndDL3027Interaction verifies that DL3014 and DL3027 work correctly
// together. DL3027 checks for "apt" (should use apt-get), while DL3014 checks
// for apt-get install without -y. Both rules should fire independently.
func TestDL3014AndDL3027Interaction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantDL3014 int
		wantDL3027 int
	}{
		{
			name: "apt install (both rules)",
			dockerfile: `FROM ubuntu
RUN apt install python`,
			wantDL3014: 0, // DL3014 is for apt-get, not apt
			wantDL3027: 1, // DL3027 fires for apt
		},
		{
			name: "apt-get install without -y (only DL3014)",
			dockerfile: `FROM ubuntu
RUN apt-get install python`,
			wantDL3014: 1, // DL3014 fires
			wantDL3027: 0, // DL3027 doesn't fire for apt-get
		},
		{
			name: "apt-get install -y (neither rule)",
			dockerfile: `FROM ubuntu
RUN apt-get install -y python`,
			wantDL3014: 0,
			wantDL3027: 0,
		},
		{
			name: "apt install -y (only DL3027)",
			dockerfile: `FROM ubuntu
RUN apt install -y python`,
			wantDL3014: 0, // DL3014 is for apt-get
			wantDL3027: 1, // DL3027 still fires (use apt-get)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			dl3014 := NewDL3014Rule()
			dl3027 := NewDL3027Rule()

			dl3014Violations := dl3014.Check(input)
			dl3027Violations := dl3027.Check(input)

			if len(dl3014Violations) != tt.wantDL3014 {
				t.Errorf("DL3014: got %d violations, want %d", len(dl3014Violations), tt.wantDL3014)
			}
			if len(dl3027Violations) != tt.wantDL3027 {
				t.Errorf("DL3027: got %d violations, want %d", len(dl3027Violations), tt.wantDL3027)
			}
		})
	}
}

func TestDL3014Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3014Rule().Metadata())
}
