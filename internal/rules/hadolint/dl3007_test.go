package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3007Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3007Rule().Metadata())
}

func TestDL3007Rule_Check(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dockerfile string
		wantCount  int
		wantCode   string
	}{
		{
			name:       "image with :latest tag",
			dockerfile: "FROM ubuntu:latest\n",
			wantCount:  1,
			wantCode:   rules.HadolintRulePrefix + "DL3007",
		},
		{
			name:       "untagged image",
			dockerfile: "FROM ubuntu\n",
			wantCount:  0, // DL3006 checks for untagged, not DL3007
		},
		{
			name:       "tagged with specific version",
			dockerfile: "FROM ubuntu:22.04\n",
			wantCount:  0,
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
			name: "multi-stage with latest",
			dockerfile: `FROM ubuntu:latest AS builder
FROM alpine:latest
`,
			wantCount: 2,
		},
		{
			name: "multi-stage mixed",
			dockerfile: `FROM ubuntu:22.04 AS builder
FROM alpine:latest
`,
			wantCount: 1,
		},
		{
			name: "multi-stage referencing stage",
			dockerfile: `FROM ubuntu:latest AS builder
FROM builder AS final
`,
			wantCount: 1, // Only first FROM uses :latest
		},
		{
			name:       "fully qualified image with :latest",
			dockerfile: "FROM docker.io/library/ubuntu:latest\n",
			wantCount:  1,
		},
		{
			name:       "fully qualified image with specific tag",
			dockerfile: "FROM docker.io/library/ubuntu:22.04\n",
			wantCount:  0,
		},
		{
			name:       "private registry with :latest",
			dockerfile: "FROM gcr.io/myproject/myimage:latest\n",
			wantCount:  1,
		},
		{
			name:       "private registry with specific tag",
			dockerfile: "FROM gcr.io/myproject/myimage:v1.0\n",
			wantCount:  0,
		},
		{
			name: "arg in image name",
			dockerfile: `ARG BASE_IMAGE=ubuntu:latest
FROM ${BASE_IMAGE}
`,
			wantCount: 0, // Can't resolve the ARG value
		},
		// Tests from hadolint/hadolint test/Hadolint/Rule/DL3007Spec.hs
		{
			name:       "explicit latest with name",
			dockerfile: "FROM debian:latest AS builder\n",
			wantCount:  1,
			wantCode:   rules.HadolintRulePrefix + "DL3007",
		},
		{
			name:       "explicit tagged with name",
			dockerfile: "FROM debian:jessie AS builder\n",
			wantCount:  0,
		},
		{
			name:       "explicit SHA pins the image",
			dockerfile: "FROM hub.docker.io/debian@sha256:7959ed6f7e35f8b1aaa06d1d8259d4ee25aa85a086d5c125480c333183f9deeb\n",
			wantCount:  0,
		},
		{
			name:       "tag and SHA - digest pins even with latest tag",
			dockerfile: "FROM hub.docker.io/debian:latest@sha256:7959ed6f7e35f8b1aaa06d1d8259d4ee25aa85a086d5c125480c333183f9deeb\n",
			wantCount:  0, // Digest pins the image, :latest is ignored
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)

			r := NewDL3007Rule()
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

func TestImageRefIsLatestTag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		image string
		want  bool
	}{
		{"ubuntu", false},
		{"ubuntu:latest", true},
		{"ubuntu:22.04", false},
		{"ubuntu@sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abc1", false},
		{"docker.io/library/ubuntu:latest", true},
		{"docker.io/library/ubuntu:22.04", false},
		{"gcr.io/project/image:latest", true},
		{"gcr.io/project/image:v1", false},
		{"localhost:5000/myimage:latest", true},
		{"localhost:5000/myimage:stable", false},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			t.Parallel()
			ref := parseImageRef(tt.image)
			if ref == nil {
				t.Fatalf("parseImageRef(%q) returned nil", tt.image)
			}
			got := ref.IsLatestTag()
			if got != tt.want {
				t.Errorf("IsLatestTag(%q) = %v, want %v", tt.image, got, tt.want)
			}
		})
	}
}

func TestImageRefFamiliarName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		image string
		want  string
	}{
		{"ubuntu", "ubuntu"},
		{"ubuntu:latest", "ubuntu"},
		{"ubuntu:22.04", "ubuntu"},
		{"docker.io/library/ubuntu:latest", "ubuntu"},
		{"gcr.io/project/myimage:v1", "gcr.io/project/myimage"},
		{"localhost:5000/myimage:latest", "localhost:5000/myimage"},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			t.Parallel()
			ref := parseImageRef(tt.image)
			if ref == nil {
				t.Fatalf("parseImageRef(%q) returned nil", tt.image)
			}
			got := ref.FamiliarName()
			if got != tt.want {
				t.Errorf("FamiliarName(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}
