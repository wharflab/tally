package hadolint

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/semantic"
)

func TestDL3043_ForbiddenOnbuildTriggers(t *testing.T) {
	tests := []struct {
		name        string
		dockerfile  string
		shouldFail  bool
		description string
	}{
		{
			name:        "ONBUILD within ONBUILD",
			dockerfile:  "ONBUILD ONBUILD RUN anything",
			shouldFail:  true,
			description: "should error when using ONBUILD within ONBUILD",
		},
		{
			name:        "FROM within ONBUILD",
			dockerfile:  "ONBUILD FROM debian:buster",
			shouldFail:  true,
			description: "should error when using FROM within ONBUILD",
		},
		{
			name:        "MAINTAINER within ONBUILD",
			dockerfile:  `ONBUILD MAINTAINER "BoJack Horseman"`,
			shouldFail:  true,
			description: "should error when using MAINTAINER within ONBUILD",
		},
		{
			name:        "ADD in ONBUILD",
			dockerfile:  "ONBUILD ADD anything anywhere",
			shouldFail:  false,
			description: "should allow ADD in ONBUILD",
		},
		{
			name:        "USER in ONBUILD",
			dockerfile:  "ONBUILD USER anything",
			shouldFail:  false,
			description: "should allow USER in ONBUILD",
		},
		{
			name:        "LABEL in ONBUILD",
			dockerfile:  `ONBUILD LABEL bla="blubb"`,
			shouldFail:  false,
			description: "should allow LABEL in ONBUILD",
		},
		{
			name:        "STOPSIGNAL in ONBUILD",
			dockerfile:  "ONBUILD STOPSIGNAL anything",
			shouldFail:  false,
			description: "should allow STOPSIGNAL in ONBUILD",
		},
		{
			name:        "COPY in ONBUILD",
			dockerfile:  "ONBUILD COPY anything anywhere",
			shouldFail:  false,
			description: "should allow COPY in ONBUILD",
		},
		{
			name:        "RUN in ONBUILD",
			dockerfile:  "ONBUILD RUN anything",
			shouldFail:  false,
			description: "should allow RUN in ONBUILD",
		},
		{
			name:        "CMD in ONBUILD",
			dockerfile:  "ONBUILD CMD anything",
			shouldFail:  false,
			description: "should allow CMD in ONBUILD",
		},
		{
			name:        "SHELL in ONBUILD",
			dockerfile:  "ONBUILD SHELL anything",
			shouldFail:  false,
			description: "should allow SHELL in ONBUILD",
		},
		{
			name:        "WORKDIR in ONBUILD",
			dockerfile:  "ONBUILD WORKDIR anything",
			shouldFail:  false,
			description: "should allow WORKDIR in ONBUILD",
		},
		{
			name:        "EXPOSE in ONBUILD",
			dockerfile:  "ONBUILD EXPOSE 69",
			shouldFail:  false,
			description: "should allow EXPOSE in ONBUILD",
		},
		{
			name:        "VOLUME in ONBUILD",
			dockerfile:  "ONBUILD VOLUME anything",
			shouldFail:  false,
			description: "should allow VOLUME in ONBUILD",
		},
		{
			name:        "ENTRYPOINT in ONBUILD",
			dockerfile:  "ONBUILD ENTRYPOINT anything",
			shouldFail:  false,
			description: "should allow ENTRYPOINT in ONBUILD",
		},
		{
			name:        "ENV in ONBUILD",
			dockerfile:  `ONBUILD ENV MYVAR="bla"`,
			shouldFail:  false,
			description: "should allow ENV in ONBUILD",
		},
		{
			name:        "ARG in ONBUILD",
			dockerfile:  "ONBUILD ARG anything",
			shouldFail:  false,
			description: "should allow ARG in ONBUILD",
		},
		{
			name:        "HEALTHCHECK in ONBUILD",
			dockerfile:  "ONBUILD HEALTHCHECK NONE",
			shouldFail:  false,
			description: "should allow HEALTHCHECK in ONBUILD",
		},
		{
			name:        "FROM outside ONBUILD",
			dockerfile:  "FROM debian:buster",
			shouldFail:  false,
			description: "should allow FROM outside of ONBUILD",
		},
		{
			name:        "MAINTAINER outside ONBUILD",
			dockerfile:  `MAINTAINER "Some Guy"`,
			shouldFail:  false,
			description: "should allow MAINTAINER outside of ONBUILD",
		},
		{
			name: "complex Dockerfile with valid ONBUILD",
			dockerfile: `FROM alpine:3.18
ONBUILD RUN apk add --no-cache git
ONBUILD COPY . /app
ONBUILD WORKDIR /app`,
			shouldFail:  false,
			description: "should not warn on valid ONBUILD usage",
		},
		{
			name: "mixed valid and invalid ONBUILD",
			dockerfile: `FROM alpine:3.18
ONBUILD RUN echo "valid"
ONBUILD FROM debian:latest
ONBUILD COPY . /app`,
			shouldFail:  true,
			description: "should error on forbidden instruction even with valid ones",
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

			// Check for DL3043 violations
			issues := model.ConstructionIssues()
			var foundDL3043 bool
			for _, issue := range issues {
				if issue.Code == DL3043Code {
					foundDL3043 = true
					break
				}
			}

			if tt.shouldFail && !foundDL3043 {
				t.Errorf("%s: expected DL3043 violation but none found", tt.description)
			}
			if !tt.shouldFail && foundDL3043 {
				t.Errorf("%s: unexpected DL3043 violation", tt.description)
			}
		})
	}
}
