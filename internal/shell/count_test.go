package shell

import (
	"testing"
)

func TestCountChainedCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant Variant
		want    int
	}{
		{
			name:    "single command",
			script:  "echo hello",
			variant: VariantBash,
			want:    1,
		},
		{
			name:    "two chained commands",
			script:  "apt-get update && apt-get install -y curl",
			variant: VariantBash,
			want:    2,
		},
		{
			name:    "three chained commands",
			script:  "apt-get update && apt-get upgrade -y && apt-get install -y vim",
			variant: VariantBash,
			want:    3,
		},
		{
			name:    "pipeline counts as one",
			script:  "cat file | grep pattern | wc -l",
			variant: VariantBash,
			want:    1,
		},
		{
			name:    "chain with pipeline",
			script:  "apt-get update && cat file | grep foo",
			variant: VariantBash,
			want:    2,
		},
		{
			name:    "semicolon separated",
			script:  "echo a; echo b; echo c",
			variant: VariantBash,
			want:    3,
		},
		{
			name:    "or chain counts as one",
			script:  "test -f file || touch file",
			variant: VariantBash,
			want:    1, // || chains are not split (set -e would change their semantics)
		},
		{
			name:    "mixed chain with or counts as one",
			script:  "cmd1 && cmd2 || cmd3",
			variant: VariantBash,
			want:    1, // Contains ||, so treated as single command
		},
		{
			name:    "empty script",
			script:  "",
			variant: VariantBash,
			want:    0,
		},
		{
			name:    "non-POSIX shell",
			script:  "echo hello",
			variant: VariantNonPOSIX,
			want:    0,
		},
		{
			name:    "if statement counts as one",
			script:  "if true; then echo yes; fi",
			variant: VariantBash,
			want:    1,
		},
		{
			name:    "multiline with continuation",
			script:  "apt-get update && \\\n    apt-get install -y \\\n    curl",
			variant: VariantBash,
			want:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CountChainedCommands(tt.script, tt.variant)
			if got != tt.want {
				t.Errorf("CountChainedCommands() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractChainedCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant Variant
		want    []string
	}{
		{
			name:    "single command",
			script:  "echo hello",
			variant: VariantBash,
			want:    []string{"echo hello"},
		},
		{
			name:    "two chained commands",
			script:  "apt-get update && apt-get install -y curl",
			variant: VariantBash,
			want:    []string{"apt-get update", "apt-get install -y curl"},
		},
		{
			name:    "three chained commands with continuation",
			script:  "apt-get update && \\\n    apt-get upgrade -y && \\\n    apt-get install -y vim",
			variant: VariantBash,
			want:    []string{"apt-get update", "apt-get upgrade -y", "apt-get install -y vim"},
		},
		{
			name:    "pipeline preserved",
			script:  "cat file | grep pattern | wc -l",
			variant: VariantBash,
			want:    []string{"cat file | grep pattern | wc -l"},
		},
		{
			name:    "non-POSIX shell returns nil",
			script:  "echo hello",
			variant: VariantNonPOSIX,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractChainedCommands(tt.script, tt.variant)
			if len(got) != len(tt.want) {
				t.Errorf("ExtractChainedCommands() returned %d commands, want %d\ngot: %v", len(got), len(tt.want), got)
				return
			}
			for i, cmd := range got {
				if cmd != tt.want[i] {
					t.Errorf("command %d = %q, want %q", i, cmd, tt.want[i])
				}
			}
		})
	}
}

func TestIsSimpleScript(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant Variant
		want    bool
	}{
		{
			name:    "simple command",
			script:  "echo hello",
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "chained commands",
			script:  "apt-get update && apt-get install -y curl",
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "pipeline",
			script:  "cat file | grep pattern",
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "if statement is complex",
			script:  "if true; then echo yes; fi",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "for loop is complex",
			script:  "for i in 1 2 3; do echo $i; done",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "while loop is complex",
			script:  "while true; do echo loop; done",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "exit breaks simplicity",
			script:  "exit 0",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "return breaks simplicity",
			script:  "return 1",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "function is complex",
			script:  "foo() { echo bar; }",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "subshell is complex",
			script:  "(cd /tmp && make)",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "non-POSIX returns false",
			script:  "echo hello",
			variant: VariantNonPOSIX,
			want:    false,
		},
		{
			name:    "or chain is not simple",
			script:  "test -f file || touch file",
			variant: VariantBash,
			want:    false, // || chains can't be converted to heredocs (set -e changes semantics)
		},
		{
			name:    "mixed and-or chain is not simple",
			script:  "cmd1 && cmd2 || cmd3",
			variant: VariantBash,
			want:    false, // Contains ||, can't be converted
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsSimpleScript(tt.script, tt.variant)
			if got != tt.want {
				t.Errorf("IsSimpleScript() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasExitCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant Variant
		want    bool
	}{
		{
			name:    "no exit",
			script:  "echo hello && echo world",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "simple exit",
			script:  "exit 0",
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "exit in chain",
			script:  "test -f file || exit 1",
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "exit in if",
			script:  "if ! test -f file; then exit 1; fi",
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "non-POSIX returns false",
			script:  "exit 0",
			variant: VariantNonPOSIX,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := HasExitCommand(tt.script, tt.variant)
			if got != tt.want {
				t.Errorf("HasExitCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsHeredocCandidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		script      string
		variant     Variant
		minCommands int
		want        bool
	}{
		{
			name:        "3 chained commands meets threshold",
			script:      "apt-get update && apt-get upgrade && apt-get install vim",
			variant:     VariantBash,
			minCommands: 3,
			want:        true,
		},
		{
			name:        "2 chained commands below threshold",
			script:      "apt-get update && apt-get install vim",
			variant:     VariantBash,
			minCommands: 3,
			want:        false,
		},
		{
			name:        "complex script with if - not candidate",
			script:      "if true; then echo yes; fi && apt-get update",
			variant:     VariantBash,
			minCommands: 2,
			want:        false,
		},
		{
			name:        "script with exit - not candidate",
			script:      "apt-get update && exit 0 && apt-get install vim",
			variant:     VariantBash,
			minCommands: 2,
			want:        false,
		},
		{
			name:        "non-POSIX shell - not candidate",
			script:      "apt-get update && apt-get install vim && apt-get clean",
			variant:     VariantNonPOSIX,
			minCommands: 3,
			want:        false,
		},
		{
			name:        "cd in chain - still candidate",
			script:      "cd /app && make && make install",
			variant:     VariantBash,
			minCommands: 3,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsHeredocCandidate(tt.script, tt.variant, tt.minCommands)
			if got != tt.want {
				t.Errorf("IsHeredocCandidate() = %v, want %v", got, tt.want)
			}
		})
	}
}
