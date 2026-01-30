package nosudo

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestRule_Metadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != rules.HadolintRulePrefix+"DL3004" {
		t.Errorf("Code = %q, want %q", meta.Code, rules.HadolintRulePrefix+"DL3004")
	}
	if meta.DefaultSeverity != rules.SeverityError {
		t.Errorf("DefaultSeverity = %v, want %v", meta.DefaultSeverity, rules.SeverityError)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
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
			name: "simple sudo usage",
			dockerfile: `FROM ubuntu:22.04
RUN sudo apt-get update
`,
			wantCount: 1,
			wantCode:  rules.HadolintRulePrefix + "DL3004",
		},
		{
			name: "sudo at start of command",
			dockerfile: `FROM ubuntu:22.04
RUN sudo apt-get install -y curl
`,
			wantCount: 1,
		},
		{
			name: "no sudo",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
`,
			wantCount: 0,
		},
		{
			name: "sudo in pipeline",
			dockerfile: `FROM ubuntu:22.04
RUN echo "password" | sudo -S apt-get update
`,
			wantCount: 1,
		},
		{
			name: "sudo with &&",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update && sudo apt-get install -y curl
`,
			wantCount: 1,
		},
		{
			name: "sudo in multiple commands",
			dockerfile: `FROM ubuntu:22.04
RUN sudo apt-get update
RUN sudo apt-get install -y curl
`,
			wantCount: 2,
		},
		{
			name: "sudo-like word in package name",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get install -y sudo-utils
`,
			wantCount: 0, // "sudo-utils" is not a sudo command
		},
		{
			name: "exec form with sudo",
			dockerfile: `FROM ubuntu:22.04
RUN ["sudo", "apt-get", "update"]
`,
			wantCount: 1,
		},
		{
			name: "exec form without sudo",
			dockerfile: `FROM ubuntu:22.04
RUN ["apt-get", "update"]
`,
			wantCount: 0,
		},
		{
			name: "multi-stage with sudo",
			dockerfile: `FROM ubuntu:22.04 AS builder
RUN sudo apt-get update

FROM alpine:3.18
RUN apk add curl
`,
			wantCount: 1,
		},
		{
			name: "sudo after semicolon",
			dockerfile: `FROM ubuntu:22.04
RUN echo "setup"; sudo apt-get update
`,
			wantCount: 1,
		},
		{
			name: "sudo in subshell",
			dockerfile: `FROM ubuntu:22.04
RUN (sudo apt-get update)
`,
			wantCount: 1,
		},
		{
			name: "sudo with env var",
			dockerfile: `FROM ubuntu:22.04
RUN DEBIAN_FRONTEND=noninteractive sudo apt-get update
`,
			wantCount: 1,
		},
		{
			name: "multiline RUN with sudo",
			dockerfile: `FROM ubuntu:22.04
RUN apt-get update \
    && sudo apt-get install -y curl
`,
			wantCount: 1,
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

func TestContainsSudo(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"sudo apt-get update", true},
		{"apt-get update", false},
		{"echo sudo", false}, // "sudo" as argument, not command
		{"apt-get install sudo", false},
		{"sudo -u user cmd", true},
		{"echo 'use sudo here'", false},
		{"DEBIAN_FRONTEND=noninteractive sudo apt", true},
		{"", false},
		{"  sudo  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := containsSudo(tt.cmd)
			if got != tt.want {
				t.Errorf("containsSudo(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestTokenizeShellCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want []string
	}{
		{"apt-get update", []string{"apt-get"}},
		{"sudo apt-get update", []string{"sudo"}}, // sudo is the command, apt-get is its argument
		{"FOO=bar apt-get update", []string{"apt-get"}},
		{"apt-get update && apt-get install curl", []string{"apt-get", "apt-get"}},
		{"apt-get update; echo done", []string{"apt-get", "echo"}},
		{"(apt-get update)", []string{"apt-get"}},
		{"echo hello | sudo tee /etc/file", []string{"echo", "sudo"}},
		{"apt-get update && sudo apt-get install", []string{"apt-get", "sudo"}},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			got := tokenizeShellCommand(tt.cmd)
			if len(got) != len(tt.want) {
				t.Errorf("tokenizeShellCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
				return
			}
			for i, g := range got {
				if g != tt.want[i] {
					t.Errorf("tokenizeShellCommand(%q)[%d] = %q, want %q", tt.cmd, i, g, tt.want[i])
				}
			}
		})
	}
}
