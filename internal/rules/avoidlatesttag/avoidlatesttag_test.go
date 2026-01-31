package avoidlatesttag

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestRule_Metadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != rules.HadolintRulePrefix+"DL3007" {
		t.Errorf("Code = %q, want %q", meta.Code, rules.HadolintRulePrefix+"DL3007")
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want %v", meta.DefaultSeverity, rules.SeverityWarning)
	}
	if meta.Category != "reproducibility" {
		t.Errorf("Category = %q, want %q", meta.Category, "reproducibility")
	}
}

func TestRule_Check(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInputWithSemantic(t, "Dockerfile", tt.dockerfile)

			r := New()
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

func TestUsesLatestTag(t *testing.T) {
	tests := []struct {
		image string
		want  bool
	}{
		{"ubuntu", false},
		{"ubuntu:latest", true},
		{"ubuntu:22.04", false},
		{"ubuntu@sha256:abc123", false},
		{"docker.io/library/ubuntu:latest", true},
		{"docker.io/library/ubuntu:22.04", false},
		{"gcr.io/project/image:latest", true},
		{"gcr.io/project/image:v1", false},
		{"localhost:5000/myimage:latest", true},
		{"localhost:5000/myimage:stable", false},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			got := usesLatestTag(tt.image)
			if got != tt.want {
				t.Errorf("usesLatestTag(%q) = %v, want %v", tt.image, got, tt.want)
			}
		})
	}
}

func TestGetImageName(t *testing.T) {
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
			got := getImageName(tt.image)
			if got != tt.want {
				t.Errorf("getImageName(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}
