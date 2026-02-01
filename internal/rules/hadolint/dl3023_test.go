package hadolint

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/semantic"
)

func TestDL3023_SelfReferencingCopy(t *testing.T) {
	tests := []struct {
		name        string
		dockerfile  string
		shouldFail  bool
		description string
	}{
		{
			name: "copying from same stage",
			dockerfile: `FROM node as foo
COPY --from=foo bar .`,
			shouldFail:  true,
			description: "should warn on copying from the same FROM stage",
		},
		{
			name: "copying from other stage",
			dockerfile: `FROM scratch as build
RUN foo
FROM node as run
COPY --from=build foo .
RUN baz`,
			shouldFail:  false,
			description: "should not warn on copying from other stages",
		},
		{
			name: "copying from previous stage",
			dockerfile: `FROM alpine as builder
RUN apk add build-base
FROM alpine as runtime
COPY --from=builder /app/bin /usr/local/bin`,
			shouldFail:  false,
			description: "should not warn on copying from previous stage",
		},
		{
			name: "case insensitive self-reference",
			dockerfile: `FROM alpine AS Builder
COPY --from=builder /app .`,
			shouldFail:  true,
			description: "should detect case-insensitive self-reference",
		},
		{
			name: "copying from external image",
			dockerfile: `FROM alpine
COPY --from=nginx:latest /etc/nginx /etc/nginx`,
			shouldFail:  false,
			description: "should not warn when copying from external image",
		},
		{
			name: "copying from numeric stage",
			dockerfile: `FROM alpine as builder
RUN build
FROM alpine
COPY --from=0 /app .`,
			shouldFail:  false,
			description: "should not warn when copying from numeric stage reference",
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

			// Check for DL3023 violations
			issues := model.ConstructionIssues()
			var foundDL3023 bool
			for _, issue := range issues {
				if issue.Code == DL3023Code {
					foundDL3023 = true
					break
				}
			}

			if tt.shouldFail && !foundDL3023 {
				t.Errorf("%s: expected DL3023 violation but none found", tt.description)
			}
			if !tt.shouldFail && foundDL3023 {
				t.Errorf("%s: unexpected DL3023 violation", tt.description)
			}
		})
	}
}
