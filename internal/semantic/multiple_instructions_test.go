package semantic

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
)

const multipleInstructionsCode = rules.BuildKitRulePrefix + "MultipleInstructionsDisallowed"

func TestMultipleInstructionsDisallowed_CMD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantLines  []int // 1-based lines where violations are expected
	}{
		// From Hadolint DL4003Spec
		{
			name:       "many cmds",
			dockerfile: "FROM debian\nCMD bash\nRUN foo\nCMD another\n",
			wantCount:  1,
			wantLines:  []int{2}, // first CMD reported (the ignored one)
		},
		{
			name: "single cmds, different stages",
			dockerfile: "FROM debian AS distro1\nCMD bash\nRUN foo\n" +
				"FROM debian AS distro2\nCMD another\n",
			wantCount: 0,
		},
		{
			name: "many cmds, different stages",
			dockerfile: "FROM debian AS distro1\nCMD bash\nRUN foo\nCMD another\n" +
				"FROM debian AS distro2\nCMD another\n",
			wantCount: 1,
			wantLines: []int{2}, // first CMD in stage1
		},
		{
			name:       "single cmd",
			dockerfile: "FROM scratch\nCMD /bin/true\n",
			wantCount:  0,
		},
		// Additional test cases
		{
			name:       "three cmds in same stage",
			dockerfile: "FROM debian\nCMD first\nCMD second\nCMD third\n",
			wantCount:  2,
			wantLines:  []int{2, 3}, // first and second CMD reported
		},
		{
			name:       "no cmd",
			dockerfile: "FROM scratch\nRUN echo hello\n",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := dockerfile.Parse(strings.NewReader(tt.dockerfile), nil)
			require.NoError(t, err)

			model := NewModel(result, nil, "Dockerfile")
			issues := model.ConstructionIssues()

			var found []Issue
			for _, issue := range issues {
				if issue.Code == multipleInstructionsCode &&
					strings.Contains(issue.Message, "CMD") {
					found = append(found, issue)
				}
			}

			assert.Len(t, found, tt.wantCount)
			for i, line := range tt.wantLines {
				if i < len(found) {
					assert.Equal(t, line, found[i].Location.Start.Line,
						"violation %d should be on line %d", i, line)
					assert.Equal(t, rules.SeverityWarning, found[i].Severity)
				}
			}
		})
	}
}

func TestMultipleInstructionsDisallowed_HEALTHCHECK(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantLines  []int
	}{
		// From Hadolint DL3012Spec
		{
			name:       "no HEALTHCHECK",
			dockerfile: "FROM scratch\n",
			wantCount:  0,
		},
		{
			name:       "one HEALTHCHECK",
			dockerfile: "FROM scratch\nHEALTHCHECK CMD /bin/bla\n",
			wantCount:  0,
		},
		{
			name: "two HEALTHCHECK in different stages",
			dockerfile: "FROM scratch\nHEALTHCHECK CMD /bin/bla1\n" +
				"FROM scratch\nHEALTHCHECK CMD /bin/bla2\n",
			wantCount: 0,
		},
		{
			name:       "two HEALTHCHECK in same stage",
			dockerfile: "FROM scratch\nHEALTHCHECK CMD /bin/bla1\nHEALTHCHECK CMD /bin/bla2\n",
			wantCount:  1,
			wantLines:  []int{2}, // first HEALTHCHECK reported (the ignored one)
		},
		// Additional test cases
		{
			name:       "three HEALTHCHECK in same stage",
			dockerfile: "FROM scratch\nHEALTHCHECK CMD /bin/check1\nHEALTHCHECK CMD /bin/check2\nHEALTHCHECK CMD /bin/check3\n",
			wantCount:  2,
			wantLines:  []int{2, 3}, // first and second reported
		},
		{
			name: "HEALTHCHECK in first stage only",
			dockerfile: "FROM node AS builder\nHEALTHCHECK CMD curl -f http://localhost/\nRUN npm install\n" +
				"FROM scratch\nCOPY --from=builder /app /app\n",
			wantCount: 0,
		},
		{
			name: "multiple HEALTHCHECK in second stage",
			dockerfile: "FROM node AS builder\nRUN npm install\n" +
				"FROM scratch\nHEALTHCHECK CMD /bin/check1\nHEALTHCHECK CMD /bin/check2\n",
			wantCount: 1,
			wantLines: []int{4}, // first HEALTHCHECK in stage 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := dockerfile.Parse(strings.NewReader(tt.dockerfile), nil)
			require.NoError(t, err)

			model := NewModel(result, nil, "Dockerfile")
			issues := model.ConstructionIssues()

			var found []Issue
			for _, issue := range issues {
				if issue.Code == multipleInstructionsCode &&
					strings.Contains(issue.Message, "HEALTHCHECK") {
					found = append(found, issue)
				}
			}

			assert.Len(t, found, tt.wantCount)
			for i, line := range tt.wantLines {
				if i < len(found) {
					assert.Equal(t, line, found[i].Location.Start.Line,
						"violation %d should be on line %d", i, line)
					assert.Equal(t, rules.SeverityWarning, found[i].Severity)
				}
			}
		})
	}
}

func TestMultipleInstructionsDisallowed_ENTRYPOINT(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantLines  []int
	}{
		// From Hadolint DL4004Spec
		{
			name:       "no cmd",
			dockerfile: "FROM busybox\n",
			wantCount:  0,
		},
		{
			name:       "many entrypoints",
			dockerfile: "FROM debian\nENTRYPOINT bash\nRUN foo\nENTRYPOINT another\n",
			wantCount:  1,
			wantLines:  []int{2},
		},
		{
			name: "single entrypoint, different stages",
			dockerfile: "FROM debian AS distro1\nENTRYPOINT bash\nRUN foo\n" +
				"FROM debian AS distro2\nENTRYPOINT another\n",
			wantCount: 0,
		},
		{
			name: "many entrypoints, different stages",
			dockerfile: "FROM debian AS distro1\nENTRYPOINT bash\nRUN foo\nENTRYPOINT another\n" +
				"FROM debian AS distro2\nENTRYPOINT another\n",
			wantCount: 1,
			wantLines: []int{2},
		},
		{
			name:       "single entry",
			dockerfile: "FROM scratch\nENTRYPOINT /bin/true\n",
			wantCount:  0,
		},
		{
			name:       "no entry",
			dockerfile: "FROM busybox\n",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := dockerfile.Parse(strings.NewReader(tt.dockerfile), nil)
			require.NoError(t, err)

			model := NewModel(result, nil, "Dockerfile")
			issues := model.ConstructionIssues()

			var found []Issue
			for _, issue := range issues {
				if issue.Code == multipleInstructionsCode &&
					strings.Contains(issue.Message, "ENTRYPOINT") {
					found = append(found, issue)
				}
			}

			assert.Len(t, found, tt.wantCount)
			for i, line := range tt.wantLines {
				if i < len(found) {
					assert.Equal(t, line, found[i].Location.Start.Line,
						"violation %d should be on line %d", i, line)
					assert.Equal(t, rules.SeverityWarning, found[i].Severity)
				}
			}
		})
	}
}
