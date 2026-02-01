package hadolint

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/semantic"
)

func TestDL3024_DuplicateStageNames(t *testing.T) {
	tests := []struct {
		name        string
		dockerfile  string
		shouldFail  bool
		description string
	}{
		{
			name: "duplicate aliases",
			dockerfile: `FROM node as foo
RUN something
FROM scratch as foo
RUN something`,
			shouldFail:  true,
			description: "should warn on duplicate stage aliases",
		},
		{
			name: "unique aliases",
			dockerfile: `FROM scratch as build
RUN foo
FROM node as run
RUN baz`,
			shouldFail:  false,
			description: "should not warn on unique stage aliases",
		},
		{
			name: "case insensitive duplicates",
			dockerfile: `FROM node as Foo
FROM scratch as foo`,
			shouldFail:  true,
			description: "should detect case-insensitive duplicate aliases",
		},
		{
			name: "no aliases",
			dockerfile: `FROM node
RUN something
FROM scratch
RUN something`,
			shouldFail:  false,
			description: "should not warn when no aliases are used",
		},
		{
			name: "single stage with alias",
			dockerfile: `FROM node as builder
RUN npm install`,
			shouldFail:  false,
			description: "should not warn with single aliased stage",
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

			// Check for DL3024 violations
			issues := model.ConstructionIssues()
			var foundDL3024 bool
			for _, issue := range issues {
				if issue.Code == DL3024Code {
					foundDL3024 = true
					break
				}
			}

			if tt.shouldFail && !foundDL3024 {
				t.Errorf("%s: expected DL3024 violation but none found", tt.description)
			}
			if !tt.shouldFail && foundDL3024 {
				t.Errorf("%s: unexpected DL3024 violation", tt.description)
			}
		})
	}
}
