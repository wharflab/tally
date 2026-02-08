package hadolint

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestDL3004Rule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewDL3004Rule().Metadata())
}

func TestDL3004Rule_Check(t *testing.T) {
	t.Parallel()
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
		// Command wrapper tests - ensure sudo is detected through wrappers
		{
			name: "sudo via env wrapper",
			dockerfile: `FROM ubuntu:22.04
RUN env sudo apt-get update
`,
			wantCount: 1,
		},
		{
			name: "sudo via nice wrapper",
			dockerfile: `FROM ubuntu:22.04
RUN nice -n 10 sudo apt-get update
`,
			wantCount: 1,
		},
		{
			name: "sudo via timeout wrapper",
			dockerfile: `FROM ubuntu:22.04
RUN timeout 60 sudo apt-get update
`,
			wantCount: 1,
		},
		{
			name: "sudo via sh -c wrapper",
			dockerfile: `FROM ubuntu:22.04
RUN sh -c 'sudo apt-get update'
`,
			wantCount: 1,
		},
		{
			name: "sudo via bash -c wrapper",
			dockerfile: `FROM ubuntu:22.04
RUN bash -c "sudo apt-get update"
`,
			wantCount: 1,
		},
		{
			name: "sudo via nested wrappers",
			dockerfile: `FROM ubuntu:22.04
RUN env nice sudo apt-get update
`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)

			r := NewDL3004Rule()
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

// TestContainsSudo verifies that shell.ContainsCommand correctly detects sudo.
// This test ensures our integration with the shell package works as expected.
func TestContainsSudo(t *testing.T) {
	t.Parallel()
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
		// Command wrapper tests
		{"env sudo apt-get", true},
		{"nice sudo apt-get", true},
		{"nice -n 10 sudo apt-get", true},
		{"timeout 60 sudo apt-get", true},
		{"sh -c 'sudo apt-get'", true},
		{"bash -c 'sudo apt-get'", true},
		{"env nice sudo apt-get", true},
	}

	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			t.Parallel()
			got := shell.ContainsCommand(tt.cmd, "sudo")
			if got != tt.want {
				t.Errorf("shell.ContainsCommand(%q, \"sudo\") = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
