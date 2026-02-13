package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3030Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
	}{
		// Original Hadolint test cases - should trigger (ruleCatches)
		{
			name: "yum install without -y",
			dockerfile: `FROM centos
RUN yum install httpd-2.4.24 && yum clean all`,
			wantCount: 1,
		},
		{
			name: "ONBUILD yum install without -y",
			dockerfile: `FROM centos
ONBUILD RUN yum install httpd-2.4.24 && yum clean all`,
			wantCount: 1,
		},

		// Original Hadolint test cases - should NOT trigger (ruleCatchesNot)
		{
			name: "yum install -y",
			dockerfile: `FROM centos
RUN yum install -y httpd-2.4.24 && yum clean all`,
			wantCount: 0,
		},
		{
			name: "bash comment not yum",
			dockerfile: `FROM centos
RUN bash -c ` + "`" + `# not even a yum command` + "`",
			wantCount: 0,
		},

		// Additional test cases
		{
			name: "yum --assumeyes install",
			dockerfile: `FROM centos
RUN yum --assumeyes install httpd`,
			wantCount: 0,
		},
		{
			name: "yum groupinstall without -y",
			dockerfile: `FROM centos
RUN yum groupinstall "Development Tools"`,
			wantCount: 1,
		},
		{
			name: "yum groupinstall -y",
			dockerfile: `FROM centos
RUN yum groupinstall -y "Development Tools"`,
			wantCount: 0,
		},
		{
			name: "yum localinstall without -y",
			dockerfile: `FROM centos
RUN yum localinstall package.rpm`,
			wantCount: 1,
		},
		{
			name: "yum localinstall -y",
			dockerfile: `FROM centos
RUN yum localinstall -y package.rpm`,
			wantCount: 0,
		},
		{
			name: "yum reinstall without -y",
			dockerfile: `FROM centos
RUN yum reinstall httpd`,
			wantCount: 1,
		},
		{
			name: "yum reinstall -y",
			dockerfile: `FROM centos
RUN yum reinstall -y httpd`,
			wantCount: 0,
		},
		{
			name: "yum update (no violation)",
			dockerfile: `FROM centos
RUN yum update`,
			wantCount: 0,
		},
		{
			name: "yum clean (no violation)",
			dockerfile: `FROM centos
RUN yum clean all`,
			wantCount: 0,
		},
		{
			name: "multiple yum install without -y",
			dockerfile: `FROM centos
RUN yum install httpd && yum install nginx`,
			wantCount: 2,
		},
		{
			name: "yum -y before subcommand",
			dockerfile: `FROM centos
RUN yum -y install httpd`,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)
			r := NewDL3030Rule()
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
				if v.RuleCode != "hadolint/DL3030" {
					t.Errorf("got rule code %q, want %q", v.RuleCode, "hadolint/DL3030")
				}
				if v.Message == "" {
					t.Error("violation message is empty")
				}
			}
		})
	}
}

func TestDL3030Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3030Rule().Metadata())
}
