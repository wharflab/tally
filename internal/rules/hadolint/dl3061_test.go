package hadolint

import (
	"testing"

	"github.com/wharflab/tally/internal/testutil"
)

func TestDL3061_InvalidInstructionOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		dockerfile  string
		shouldFail  bool
		description string
	}{
		{
			name:        "LABEL before FROM",
			dockerfile:  "LABEL foo=bar\nFROM foo",
			shouldFail:  true,
			description: "should warn on LABEL before FROM",
		},
		{
			name:        "FROM before LABEL",
			dockerfile:  "FROM foo\nLABEL foo=bar",
			shouldFail:  false,
			description: "should not warn on FROM before LABEL",
		},
		{
			name:        "ARG then FROM then LABEL",
			dockerfile:  "ARG A=B\nFROM foo\nLABEL foo=bar",
			shouldFail:  false,
			description: "should not warn on ARG then FROM then LABEL",
		},
		{
			name: "pragma before FROM",
			dockerfile: `# syntax = docker/dockerfile:1.0-experimental
FROM node:16-alpine3.13`,
			shouldFail:  false,
			description: "should not warn on pragma/comment before FROM",
		},
		{
			name:        "FROM then ARG then RUN",
			dockerfile:  "FROM foo\nARG A=B\nRUN echo bla",
			shouldFail:  false,
			description: "should not warn on FROM then ARG then RUN",
		},
		{
			name:        "RUN before FROM",
			dockerfile:  "RUN echo bad\nFROM alpine",
			shouldFail:  true,
			description: "should warn on RUN before FROM",
		},
		{
			name:        "COPY before FROM",
			dockerfile:  "COPY . /app\nFROM alpine",
			shouldFail:  true,
			description: "should warn on COPY before FROM",
		},
		{
			name:        "ENV before FROM",
			dockerfile:  "ENV FOO=bar\nFROM alpine",
			shouldFail:  true,
			description: "should warn on ENV before FROM",
		},
		{
			name:        "Multiple ARG before FROM",
			dockerfile:  "ARG VERSION=latest\nARG BASE=alpine\nFROM ${BASE}:${VERSION}",
			shouldFail:  false,
			description: "should allow multiple ARG before FROM",
		},
		{
			name: "Comment and ARG before FROM",
			dockerfile: `# Build arguments
ARG VERSION=1.0
# Base image
FROM alpine:${VERSION}`,
			shouldFail:  false,
			description: "should allow comments and ARG before FROM",
		},
		{
			name:        "Empty lines before FROM",
			dockerfile:  "\n\nFROM alpine",
			shouldFail:  false,
			description: "should allow empty lines before FROM",
		},
		{
			name:        "WORKDIR before FROM",
			dockerfile:  "WORKDIR /app\nFROM alpine",
			shouldFail:  true,
			description: "should warn on WORKDIR before FROM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			violations := NewDL3061Rule().Check(testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile))
			var foundDL3061 bool
			for _, violation := range violations {
				if violation.RuleCode == DL3061Code {
					foundDL3061 = true
					break
				}
			}

			if tt.shouldFail && !foundDL3061 {
				t.Errorf("%s: expected DL3061 violation but none found", tt.description)
			}
			if !tt.shouldFail && foundDL3061 {
				t.Errorf("%s: unexpected DL3061 violation", tt.description)
			}
		})
	}
}
