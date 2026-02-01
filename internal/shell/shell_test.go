package shell

import (
	"slices"
	"testing"

	"mvdan.cc/sh/v3/syntax"
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

func TestCommandWrappers(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "env wrapper",
			script: "env sudo apt-get update",
			want:   []string{"env", "sudo"},
		},
		{
			name:   "env with assignments",
			script: "env FOO=bar BAZ=qux sudo apt-get",
			want:   []string{"env", "sudo"},
		},
		{
			name:   "env -i wrapper",
			script: "env -i PATH=/bin sudo apt-get",
			want:   []string{"env", "sudo"},
		},
		{
			name:   "nice wrapper",
			script: "nice sudo apt-get update",
			want:   []string{"nice", "sudo"},
		},
		{
			name:   "nice with -n flag",
			script: "nice -n 10 sudo apt-get",
			want:   []string{"nice", "sudo"},
		},
		{
			name:   "nohup wrapper",
			script: "nohup sudo apt-get &",
			want:   []string{"nohup", "sudo"},
		},
		{
			name:   "timeout wrapper",
			script: "timeout 60 sudo apt-get",
			want:   []string{"timeout", "sudo"},
		},
		{
			name:   "xargs wrapper",
			script: "xargs sudo rm",
			want:   []string{"xargs", "sudo"},
		},
		{
			name:   "nested wrappers",
			script: "env nice sudo apt-get",
			want:   []string{"env", "nice", "sudo"},
		},
		{
			name:   "command builtin",
			script: "command sudo apt-get",
			want:   []string{"command", "sudo"},
		},
		{
			name:   "exec builtin",
			script: "exec sudo apt-get",
			want:   []string{"exec", "sudo"},
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

func TestShellWrappers(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "sh -c wrapper",
			script: "sh -c 'sudo apt-get update'",
			want:   []string{"sh", "sudo"},
		},
		{
			name:   "bash -c wrapper",
			script: "bash -c 'sudo apt-get update'",
			want:   []string{"bash", "sudo"},
		},
		{
			name:   "sh -c with double quotes",
			script: `sh -c "sudo apt-get update"`,
			want:   []string{"sh", "sudo"},
		},
		{
			name:   "bash -ec (combined flags)",
			script: "bash -ec 'sudo apt-get'",
			want:   []string{"bash", "sudo"},
		},
		{
			name:   "nested shell in env",
			script: "env sh -c 'sudo apt-get'",
			want:   []string{"env", "sh", "sudo"},
		},
		{
			name:   "sh -c with multiple commands",
			script: "sh -c 'apt-get update && sudo apt-get install'",
			want:   []string{"sh", "apt-get", "sudo"},
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
		// Command wrapper tests
		{"env sudo apt-get", "sudo", true},
		{"nice sudo apt-get", "sudo", true},
		{"sh -c 'sudo apt-get'", "sudo", true},
		{"bash -c 'sudo apt-get'", "sudo", true},
		{"timeout 60 sudo apt-get", "sudo", true},
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
		{"powershell", VariantNonPOSIX}, // Non-POSIX shells
		{"pwsh", VariantNonPOSIX},
		{"cmd", VariantNonPOSIX},
		{"cmd.exe", VariantNonPOSIX},
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
		{"powershell", []string{"powershell", "-Command"}, VariantNonPOSIX}, // non-POSIX shell
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

	// Mksh variant should also parse correctly
	mkshScript := "print hello && echo world"
	mkshCmds := CommandNamesWithVariant(mkshScript, VariantMksh)
	if len(mkshCmds) != 2 || mkshCmds[0] != "print" || mkshCmds[1] != "echo" {
		t.Errorf("Mksh variant parsing failed: got %v, want [print echo]", mkshCmds)
	}
}

func TestContainsCommandWithVariant(t *testing.T) {
	tests := []struct {
		script  string
		command string
		variant Variant
		want    bool
	}{
		{"wget https://example.com", "wget", VariantBash, true},
		{"curl -o file https://example.com", "curl", VariantPOSIX, true},
		{"[[ -f /etc/foo ]] && echo hello", "echo", VariantBash, true},
		{"print hello", "print", VariantMksh, true},
		{"echo wget", "wget", VariantBash, false}, // wget is an argument
		{"", "wget", VariantBash, false},
	}

	for _, tt := range tests {
		name := tt.script + "_" + tt.command
		if len(name) > 50 {
			name = name[:50]
		}
		t.Run(name, func(t *testing.T) {
			got := ContainsCommandWithVariant(tt.script, tt.command, tt.variant)
			if got != tt.want {
				t.Errorf("ContainsCommandWithVariant(%q, %q, %v) = %v, want %v",
					tt.script, tt.command, tt.variant, got, tt.want)
			}
		})
	}
}

func TestSimpleCommandNames(t *testing.T) {
	// Test the fallback parser by calling it directly
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
			name:   "with operators",
			script: "apt-get update && apt-get install curl",
			want:   []string{"apt-get", "apt-get"},
		},
		{
			name:   "with pipe",
			script: "curl http://example.com | grep pattern",
			want:   []string{"curl", "grep"},
		},
		{
			name:   "with semicolon",
			script: "echo hello; echo world",
			want:   []string{"echo", "echo"},
		},
		{
			name:   "with env var assignment",
			script: "FOO=bar baz",
			want:   []string{"baz"},
		},
		{
			name:   "with subshell",
			script: "(apt-get update)",
			want:   []string{"apt-get"},
		},
		{
			name:   "with command substitution",
			script: "echo $(which curl)",
			want:   []string{"echo", "which"},
		},
		{
			name:   "with full path",
			script: "/usr/bin/wget http://example.com",
			want:   []string{"wget"},
		},
		{
			name:   "empty script",
			script: "",
			want:   nil,
		},
		{
			name:   "multiline with continuation",
			script: "apt-get \\\nupdate",
			want:   []string{"apt-get"},
		},
		{
			name:   "skip flags",
			script: "-y apt-get install curl",
			want:   []string{"apt-get"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := simpleCommandNames(tt.script)
			if !slices.Equal(got, tt.want) {
				t.Errorf("simpleCommandNames(%q) = %v, want %v", tt.script, got, tt.want)
			}
		})
	}
}

func TestToLangVariant(t *testing.T) {
	tests := []struct {
		variant Variant
		want    syntax.LangVariant
	}{
		{VariantBash, syntax.LangBash},
		{VariantPOSIX, syntax.LangPOSIX},
		{VariantMksh, syntax.LangMirBSDKorn},
		{VariantNonPOSIX, syntax.LangBash}, // NonPOSIX falls back to Bash for parsing
		{Variant(99), syntax.LangBash},     // Unknown variant defaults to Bash
	}

	for _, tt := range tests {
		got := tt.variant.toLangVariant()
		if got != tt.want {
			t.Errorf("Variant(%d).toLangVariant() = %v, want %v", tt.variant, got, tt.want)
		}
	}
}

func TestIsNonPOSIX(t *testing.T) {
	tests := []struct {
		variant Variant
		want    bool
	}{
		{VariantBash, false},
		{VariantPOSIX, false},
		{VariantMksh, false},
		{VariantNonPOSIX, true},
	}

	for _, tt := range tests {
		got := tt.variant.IsNonPOSIX()
		if got != tt.want {
			t.Errorf("Variant(%d).IsNonPOSIX() = %v, want %v", tt.variant, got, tt.want)
		}
	}
}
