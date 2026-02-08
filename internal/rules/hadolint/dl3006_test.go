package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3006Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3006Rule().Metadata())
}

func TestDL3006Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		{
			name:       "untagged image",
			dockerfile: "FROM ubuntu\n",
			wantCount:  1,
			wantCode:   rules.HadolintRulePrefix + "DL3006",
		},
		// Test from hadolint: "no untagged with name"
		{
			name:       "untagged image with AS name",
			dockerfile: "FROM debian AS builder\n",
			wantCount:  1,
			wantCode:   rules.HadolintRulePrefix + "DL3006",
		},
		{
			name:       "tagged with version",
			dockerfile: "FROM ubuntu:22.04\n",
			wantCount:  0,
		},
		{
			name:       "tagged with latest",
			dockerfile: "FROM ubuntu:latest\n",
			wantCount:  0, // DL3007 checks for :latest, not DL3006
		},
		{
			name:       "image with digest",
			dockerfile: "FROM ubuntu@sha256:abcdef1234567890\n",
			wantCount:  0,
		},
		{
			name:       "scratch base image",
			dockerfile: "FROM scratch\n",
			wantCount:  0,
		},
		{
			name: "multi-stage with untagged",
			dockerfile: `FROM ubuntu AS builder
FROM alpine
`,
			wantCount: 2,
		},
		{
			name: "multi-stage with mixed tags",
			dockerfile: `FROM ubuntu:22.04 AS builder
FROM alpine
`,
			wantCount: 1,
		},
		{
			name: "multi-stage referencing stage",
			dockerfile: `FROM ubuntu AS builder
FROM builder AS final
`,
			wantCount: 1, // Only first FROM is untagged
		},
		{
			name:       "fully qualified image without tag",
			dockerfile: "FROM docker.io/library/ubuntu\n",
			wantCount:  1,
		},
		{
			name:       "fully qualified image with tag",
			dockerfile: "FROM docker.io/library/ubuntu:22.04\n",
			wantCount:  0,
		},
		{
			name:       "private registry without tag",
			dockerfile: "FROM gcr.io/myproject/myimage\n",
			wantCount:  1,
		},
		{
			name:       "private registry with tag",
			dockerfile: "FROM gcr.io/myproject/myimage:v1.0\n",
			wantCount:  0,
		},
		{
			name: "arg in image name",
			dockerfile: `ARG BASE_IMAGE=ubuntu
FROM ${BASE_IMAGE}
`,
			wantCount: 0, // Can't know the resolved value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)

			r := NewDL3006Rule()
			violations := r.Check(input)

			if len(violations) != tt.wantCount {
				t.Errorf("got %d violations, want %d", len(violations), tt.wantCount)
				for _, v := range violations {
					t.Logf("  - %s: %s", v.RuleCode, v.Message)
				}
			}

			if tt.wantCode != "" && len(violations) > 0 {
				if violations[0].RuleCode != tt.wantCode {
					t.Errorf("RuleCode = %q, want %q", violations[0].RuleCode, tt.wantCode)
				}
			}
		})
	}
}

func TestImageRefHasExplicitVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		image string
		want  bool
	}{
		{"ubuntu", false},
		{"ubuntu:latest", true},
		{"ubuntu:22.04", true},
		{"ubuntu@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1", true},
		{"docker.io/library/ubuntu", false},
		{"docker.io/library/ubuntu:22.04", true},
		{"gcr.io/project/image", false},
		{"gcr.io/project/image:v1", true},
		{"localhost:5000/myimage", false},
		{"localhost:5000/myimage:latest", true},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			t.Parallel()
			ref := parseImageRef(tt.image)
			if ref == nil {
				t.Fatalf("parseImageRef(%q) returned nil", tt.image)
			}
			got := ref.HasExplicitVersion()
			if got != tt.want {
				t.Errorf("HasExplicitVersion(%q) = %v, want %v", tt.image, got, tt.want)
			}
		})
	}
}
