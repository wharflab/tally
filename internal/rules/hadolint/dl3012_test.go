package hadolint

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/semantic"
)

func TestDL3012_MultipleHealthcheck(t *testing.T) {
	tests := []struct {
		name        string
		dockerfile  string
		shouldFail  bool
		description string
	}{
		{
			name:        "no HEALTHCHECK",
			dockerfile:  "FROM scratch",
			shouldFail:  false,
			description: "should not warn with no HEALTHCHECK instruction",
		},
		{
			name: "one HEALTHCHECK",
			dockerfile: `FROM scratch
HEALTHCHECK CMD /bin/bla`,
			shouldFail:  false,
			description: "should not warn with one HEALTHCHECK instruction",
		},
		{
			name: "two HEALTHCHECK in different stages",
			dockerfile: `FROM scratch
HEALTHCHECK CMD /bin/bla1
FROM scratch
HEALTHCHECK CMD /bin/bla2`,
			shouldFail:  false,
			description: "should not warn with HEALTHCHECK in separate stages",
		},
		{
			name: "two HEALTHCHECK in same stage",
			dockerfile: `FROM scratch
HEALTHCHECK CMD /bin/bla1
HEALTHCHECK CMD /bin/bla2`,
			shouldFail:  true,
			description: "should warn with multiple HEALTHCHECK in same stage",
		},
		{
			name: "three HEALTHCHECK in same stage",
			dockerfile: `FROM scratch
HEALTHCHECK CMD /bin/check1
HEALTHCHECK CMD /bin/check2
HEALTHCHECK CMD /bin/check3`,
			shouldFail:  true,
			description: "should warn with three HEALTHCHECK in same stage",
		},
		{
			name: "HEALTHCHECK in first stage only",
			dockerfile: `FROM node as builder
HEALTHCHECK --interval=30s CMD curl -f http://localhost/ || exit 1
RUN npm install
FROM scratch
COPY --from=builder /app /app`,
			shouldFail:  false,
			description: "should not warn with single HEALTHCHECK in multi-stage build",
		},
		{
			name: "multiple HEALTHCHECK in second stage",
			dockerfile: `FROM node as builder
RUN npm install
FROM scratch
HEALTHCHECK CMD /bin/check1
HEALTHCHECK CMD /bin/check2`,
			shouldFail:  true,
			description: "should warn with multiple HEALTHCHECK in second stage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse Dockerfile
			result, err := dockerfile.Parse(strings.NewReader(tt.dockerfile), nil)
			if err != nil {
				t.Fatalf("failed to parse Dockerfile: %v", err)
			}

			// Build semantic model
			model := semantic.NewModel(result, nil, "Dockerfile")

			// Check for DL3012 violations
			issues := model.ConstructionIssues()
			var foundDL3012 bool
			for _, issue := range issues {
				if issue.Code == DL3012Code {
					foundDL3012 = true
					break
				}
			}

			if tt.shouldFail && !foundDL3012 {
				t.Errorf("%s: expected DL3012 violation but none found", tt.description)
			}
			if !tt.shouldFail && foundDL3012 {
				t.Errorf("%s: unexpected DL3012 violation", tt.description)
			}
		})
	}
}
