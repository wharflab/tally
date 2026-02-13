package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3034Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Original Hadolint test cases - should trigger (ruleCatches)
		{
			name: "zypper install without non-interactive",
			dockerfile: `FROM opensuse
RUN zypper install httpd=2.4.24 && zypper clean`,
			wantCount: 1,
		},
		{
			name: "ONBUILD zypper install without non-interactive",
			dockerfile: `FROM opensuse
ONBUILD RUN zypper install httpd=2.4.24 && zypper clean`,
			wantCount: 1,
		},

		// Original Hadolint test cases - should NOT trigger (ruleCatchesNot)
		{
			name: "zypper install -n",
			dockerfile: `FROM opensuse
RUN zypper install -n httpd=2.4.24 && zypper clean`,
			wantCount: 0,
		},
		{
			name: "zypper install --non-interactive",
			dockerfile: `FROM opensuse
RUN zypper install --non-interactive httpd=2.4.24 && zypper clean`,
			wantCount: 0,
		},
		{
			name: "zypper install -y",
			dockerfile: `FROM opensuse
RUN zypper install -y httpd=2.4.24 && zypper clean`,
			wantCount: 0,
		},
		{
			name: "zypper install --no-confirm",
			dockerfile: `FROM opensuse
RUN zypper install --no-confirm httpd=2.4.24 && zypper clean`,
			wantCount: 0,
		},

		// Additional test cases for subcommand aliases
		{
			name: "zypper in (alias) without -n",
			dockerfile: `FROM opensuse
RUN zypper in httpd`,
			wantCount: 1,
		},
		{
			name: "zypper in -n",
			dockerfile: `FROM opensuse
RUN zypper in -n httpd`,
			wantCount: 0,
		},
		{
			name: "zypper remove without -n",
			dockerfile: `FROM opensuse
RUN zypper remove httpd`,
			wantCount: 1,
		},
		{
			name: "zypper rm without -n",
			dockerfile: `FROM opensuse
RUN zypper rm httpd`,
			wantCount: 1,
		},
		{
			name: "zypper source-install without -n",
			dockerfile: `FROM opensuse
RUN zypper source-install httpd`,
			wantCount: 1,
		},
		{
			name: "zypper si without -n",
			dockerfile: `FROM opensuse
RUN zypper si httpd`,
			wantCount: 1,
		},
		{
			name: "zypper patch without -n",
			dockerfile: `FROM opensuse
RUN zypper patch`,
			wantCount: 1,
		},
		{
			name: "zypper patch -n",
			dockerfile: `FROM opensuse
RUN zypper patch -n`,
			wantCount: 0,
		},
		{
			name: "zypper clean (no violation)",
			dockerfile: `FROM opensuse
RUN zypper clean`,
			wantCount: 0,
		},
		{
			name: "zypper refresh (no violation)",
			dockerfile: `FROM opensuse
RUN zypper refresh`,
			wantCount: 0,
		},
		{
			name: "multiple zypper install without -n",
			dockerfile: `FROM opensuse
RUN zypper install httpd && zypper install nginx`,
			wantCount: 2,
		},
		{
			name: "zypper -n before subcommand",
			dockerfile: `FROM opensuse
RUN zypper -n install httpd`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)
			r := NewDL3034Rule()
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
				if v.RuleCode != "hadolint/DL3034" {
					t.Errorf("got rule code %q, want %q", v.RuleCode, "hadolint/DL3034")
				}
				if v.Message == "" {
					t.Error("violation message is empty")
				}
			}
		})
	}
}

func TestDL3034Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3034Rule().Metadata())
}
