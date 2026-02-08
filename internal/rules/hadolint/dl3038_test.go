package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3038Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Original Hadolint test cases - should trigger (ruleCatches)
		{
			name: "dnf install without -y",
			dockerfile: `FROM fedora
RUN dnf install httpd-2.4.24 && dnf clean all`,
			wantCount: 1,
		},
		{
			name: "microdnf install without -y",
			dockerfile: `FROM fedora
RUN microdnf install httpd-2.4.24 && microdnf clean all`,
			wantCount: 1,
		},
		{
			name: "ONBUILD dnf install without -y",
			dockerfile: `FROM fedora
ONBUILD RUN dnf install httpd-2.4.24 && dnf clean all`,
			wantCount: 0, // ONBUILD not yet supported
		},
		{
			name: "ONBUILD microdnf install without -y",
			dockerfile: `FROM fedora
ONBUILD RUN microdnf install httpd-2.4.24 && microdnf clean all`,
			wantCount: 0, // ONBUILD not yet supported
		},

		// Original Hadolint test cases - should NOT trigger (ruleCatchesNot)
		{
			name: "dnf install -y",
			dockerfile: `FROM fedora
RUN dnf install -y httpd-2.4.24 && dnf clean all`,
			wantCount: 0,
		},
		{
			name: "microdnf install -y",
			dockerfile: `FROM fedora
RUN microdnf install -y httpd-2.4.24 && microdnf clean all`,
			wantCount: 0,
		},
		{
			name: "notdnf (similar name, not dnf)",
			dockerfile: `FROM fedora
RUN notdnf install httpd`,
			wantCount: 0,
		},

		// Additional test cases
		{
			name: "dnf --assumeyes install",
			dockerfile: `FROM fedora
RUN dnf --assumeyes install httpd`,
			wantCount: 0,
		},
		{
			name: "dnf groupinstall without -y",
			dockerfile: `FROM fedora
RUN dnf groupinstall "Development Tools"`,
			wantCount: 1,
		},
		{
			name: "dnf groupinstall -y",
			dockerfile: `FROM fedora
RUN dnf groupinstall -y "Development Tools"`,
			wantCount: 0,
		},
		{
			name: "dnf localinstall without -y",
			dockerfile: `FROM fedora
RUN dnf localinstall package.rpm`,
			wantCount: 1,
		},
		{
			name: "dnf localinstall -y",
			dockerfile: `FROM fedora
RUN dnf localinstall -y package.rpm`,
			wantCount: 0,
		},
		{
			name: "dnf update (no violation)",
			dockerfile: `FROM fedora
RUN dnf update`,
			wantCount: 0,
		},
		{
			name: "dnf clean (no violation)",
			dockerfile: `FROM fedora
RUN dnf clean all`,
			wantCount: 0,
		},
		{
			name: "microdnf clean (no violation)",
			dockerfile: `FROM fedora
RUN microdnf clean all`,
			wantCount: 0,
		},
		{
			name: "multiple dnf install without -y",
			dockerfile: `FROM fedora
RUN dnf install httpd && dnf install nginx`,
			wantCount: 2,
		},
		{
			name: "mixed dnf and microdnf without -y",
			dockerfile: `FROM fedora
RUN dnf install httpd && microdnf install nginx`,
			wantCount: 2,
		},
		{
			name: "dnf -y before subcommand",
			dockerfile: `FROM fedora
RUN dnf -y install httpd`,
			wantCount: 0,
		},
		{
			name: "microdnf -y before subcommand",
			dockerfile: `FROM fedora
RUN microdnf -y install httpd`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewDL3038Rule()
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
				if v.RuleCode != "hadolint/DL3038" {
					t.Errorf("got rule code %q, want %q", v.RuleCode, "hadolint/DL3038")
				}
				if v.Message == "" {
					t.Error("violation message is empty")
				}
			}
		})
	}
}

func TestDL3038Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3038Rule().Metadata())
}

// TestDL3038_AutoFix verifies that DL3038 provides auto-fix suggestions.
func TestDL3038_AutoFix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		dockerfile    string
		wantFix       bool
		wantInsertCol int
		wantNewText   string
	}{
		{
			name: "simple dnf install",
			dockerfile: `FROM fedora
RUN dnf install httpd`,
			wantFix:       true,
			wantInsertCol: 15, // After "install"
			wantNewText:   " -y",
		},
		{
			name: "microdnf install with package",
			dockerfile: `FROM fedora
RUN microdnf install curl wget`,
			wantFix:       true,
			wantInsertCol: 20, // After "install" (RUN=0-2, space=3, microdnf=4-11, space=12, install=13-19, end=20)
			wantNewText:   " -y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			r := NewDL3038Rule()
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
