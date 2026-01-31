package shell

import (
	"slices"
	"testing"
)

func TestCommandNames(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "simple command",
			script: "apt-get update",
			want:   []string{"apt-get"},
		},
		{
			name:   "command with args",
			script: "wget https://example.com/file",
			want:   []string{"wget"},
		},
		{
			name:   "pipeline",
			script: "echo hello | grep h",
			want:   []string{"echo", "grep"},
		},
		{
			name:   "command sequence with &&",
			script: "apt-get update && apt-get install -y curl",
			want:   []string{"apt-get", "apt-get"},
		},
		{
			name:   "command sequence with ;",
			script: "apt-get update; echo done",
			want:   []string{"apt-get", "echo"},
		},
		{
			name:   "subshell",
			script: "(apt-get update)",
			want:   []string{"apt-get"},
		},
		{
			name:   "command substitution",
			script: "echo $(which curl)",
			want:   []string{"echo", "which"},
		},
		{
			name:   "with environment variable assignment",
			script: "DEBIAN_FRONTEND=noninteractive apt-get install -y curl",
			want:   []string{"apt-get"},
		},
		{
			name:   "full path to command",
			script: "/usr/bin/wget https://example.com/file",
			want:   []string{"wget"},
		},
		{
			name:   "if statement",
			script: "if [ -f /etc/foo ]; then cat /etc/foo; fi",
			want:   []string{"[", "cat"},
		},
		{
			name:   "for loop",
			script: "for f in *.txt; do cat $f; done",
			want:   []string{"cat"},
		},
		{
			name:   "heredoc",
			script: "cat <<EOF\nhello\nEOF",
			want:   []string{"cat"},
		},
		{
			name:   "multiline with continuation",
			script: "apt-get update \\\n    && apt-get install -y curl",
			want:   []string{"apt-get", "apt-get"},
		},
		{
			name:   "wget and curl together",
			script: "wget https://example.com/file1 && curl -o file2 https://example.com/file2",
			want:   []string{"wget", "curl"},
		},
		{
			name:   "quoted command argument",
			script: `echo "wget is installed"`,
			want:   []string{"echo"},
		},
		{
			name:   "complex pipeline",
			script: "curl -s https://example.com | grep pattern | wc -l",
			want:   []string{"curl", "grep", "wc"},
		},
		{
			name:   "or operator",
			script: "wget file || curl file",
			want:   []string{"wget", "curl"},
		},
		{
			name:   "empty script",
			script: "",
			want:   nil,
		},
		{
			name:   "only whitespace",
			script: "   \n\t  ",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CommandNames(tt.script)
			if !slices.Equal(got, tt.want) {
				t.Errorf("CommandNames(%q) = %v, want %v", tt.script, got, tt.want)
			}
		})
	}
}

func TestContainsCommand(t *testing.T) {
	tests := []struct {
		script  string
		command string
		want    bool
	}{
		{"wget https://example.com", "wget", true},
		{"curl -o file https://example.com", "curl", true},
		{"apt-get install curl", "curl", false}, // curl is an argument, not a command
		{"echo wget", "wget", false},            // wget is an argument, not a command
		{"/usr/bin/wget file", "wget", true},
		{"DEBIAN_FRONTEND=noninteractive apt-get install", "apt-get", true},
		{"", "wget", false},
	}

	for _, tt := range tests {
		t.Run(tt.script+"_"+tt.command, func(t *testing.T) {
			got := ContainsCommand(tt.script, tt.command)
			if got != tt.want {
				t.Errorf("ContainsCommand(%q, %q) = %v, want %v", tt.script, tt.command, got, tt.want)
			}
		})
	}
}

func TestVariantFromShell(t *testing.T) {
	tests := []struct {
		shell string
		want  Variant
	}{
		{"bash", VariantBash},
		{"Bash", VariantBash},
		{"/bin/bash", VariantBash},
		{"/usr/bin/bash", VariantBash},
		{"sh", VariantPOSIX},
		{"/bin/sh", VariantPOSIX},
		{"dash", VariantPOSIX},
		{"/bin/dash", VariantPOSIX},
		{"ash", VariantPOSIX},
		{"/bin/ash", VariantPOSIX},
		{"mksh", VariantMksh},
		{"/bin/mksh", VariantMksh},
		{"ksh", VariantMksh},
		{"/bin/ksh", VariantMksh},
		{"zsh", VariantBash}, // zsh treated as bash-like
		{"/bin/zsh", VariantBash},
		{"unknown", VariantBash}, // unknown defaults to bash
		{"", VariantBash},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			got := VariantFromShell(tt.shell)
			if got != tt.want {
				t.Errorf("VariantFromShell(%q) = %v, want %v", tt.shell, got, tt.want)
			}
		})
	}
}

func TestVariantFromShellCmd(t *testing.T) {
	tests := []struct {
		name     string
		shellCmd []string
		want     Variant
	}{
		{"default bash", []string{"/bin/bash", "-c"}, VariantBash},
		{"default sh", []string{"/bin/sh", "-c"}, VariantPOSIX},
		{"powershell", []string{"powershell", "-Command"}, VariantBash}, // unknown -> bash
		{"empty", []string{}, VariantBash},
		{"nil", nil, VariantBash},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VariantFromShellCmd(tt.shellCmd)
			if got != tt.want {
				t.Errorf("VariantFromShellCmd(%v) = %v, want %v", tt.shellCmd, got, tt.want)
			}
		})
	}
}

func TestCommandNamesWithVariant(t *testing.T) {
	// Test that different variants parse correctly
	// Bash-specific syntax like [[ ]] should work with VariantBash
	bashScript := "[[ -f /etc/foo ]] && echo exists"
	bashCmds := CommandNamesWithVariant(bashScript, VariantBash)
	if len(bashCmds) != 1 || bashCmds[0] != "echo" {
		t.Errorf("Bash variant parsing failed: got %v, want [echo]", bashCmds)
	}

	// POSIX script should work with VariantPOSIX
	posixScript := "[ -f /etc/foo ] && echo exists"
	posixCmds := CommandNamesWithVariant(posixScript, VariantPOSIX)
	if len(posixCmds) != 2 || posixCmds[0] != "[" || posixCmds[1] != "echo" {
		t.Errorf("POSIX variant parsing failed: got %v, want [[ echo]", posixCmds)
	}
}
