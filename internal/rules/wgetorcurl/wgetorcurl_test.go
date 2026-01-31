package wgetorcurl

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestRule_Metadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != rules.HadolintRulePrefix+"DL4001" {
		t.Errorf("Code = %q, want %q", meta.Code, rules.HadolintRulePrefix+"DL4001")
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want %v", meta.DefaultSeverity, rules.SeverityWarning)
	}
	if meta.Category != "maintainability" {
		t.Errorf("Category = %q, want %q", meta.Category, "maintainability")
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
			name: "only wget is fine",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/file
`,
			wantCount: 0,
		},
		{
			name: "only curl is fine",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl -o file https://example.com/file
`,
			wantCount: 0,
		},
		{
			name: "both wget and curl",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/file1
RUN curl -o file2 https://example.com/file2
`,
			wantCount: 1, // One violation for curl
			wantCode:  rules.HadolintRulePrefix + "DL4001",
		},
		{
			name: "wget and curl in same RUN",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://example.com/file1 && curl https://example.com/file2
`,
			wantCount: 1,
		},
		{
			name: "no wget or curl",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y vim
`,
			wantCount: 0,
		},
		{
			name: "wget-like package name",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y curl
RUN apt-get install -y wget-doc
`,
			wantCount: 0, // wget-doc is not wget
		},
		{
			name: "multi-stage both tools",
			dockerfile: `FROM ubuntu:22.04 AS builder
RUN wget https://example.com/source.tar.gz

FROM alpine:3.18
RUN curl -o /tmp/file https://example.com/file
`,
			wantCount: 1, // curl is flagged
		},
		{
			name: "wget with full path",
			dockerfile: `FROM ubuntu:22.04
RUN /usr/bin/wget https://example.com/file1
RUN curl https://example.com/file2
`,
			wantCount: 1,
		},
		{
			name: "multiple curl usages flagged",
			dockerfile: `FROM ubuntu:22.04
RUN wget https://example.com/file1
RUN curl https://example.com/file2
RUN curl https://example.com/file3
`,
			wantCount: 2, // Both curl usages are flagged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

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

func TestRule_Check_SmartMessages(t *testing.T) {
	tests := []struct {
		name            string
		dockerfile      string
		wantCount       int
		wantMsgContains string
	}{
		{
			name: "curl installed, wget used - recommend curl",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "curl is installed",
		},
		{
			name: "wget installed, curl used - recommend wget",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y wget
RUN wget https://example.com/file1
RUN curl https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "wget is installed",
		},
		{
			name: "both installed - mention both",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl wget
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "both wget and curl are installed",
		},
		{
			name: "neither installed - generic message",
			dockerfile: `FROM ubuntu:22.04
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "both wget and curl are used",
		},
		{
			name: "apk add curl, wget used",
			dockerfile: `FROM alpine:3.18
RUN apk add --no-cache curl
RUN curl https://example.com/file1
RUN wget https://example.com/file2
`,
			wantCount:       1,
			wantMsgContains: "curl is installed",
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
				return
			}

			if tt.wantMsgContains != "" && len(violations) > 0 {
				if !strings.Contains(violations[0].Message, tt.wantMsgContains) {
					t.Errorf("Message %q should contain %q", violations[0].Message, tt.wantMsgContains)
				}
			}
		})
	}
}

