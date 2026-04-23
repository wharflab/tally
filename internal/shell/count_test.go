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
			name:    "or-exit guard does not collapse chain",
			script:  "cmd1 && cmd2 || exit",
			variant: VariantBash,
			want:    2,
		},
		{
			name:    "empty script",
			script:  "",
			variant: VariantBash,
			want:    0,
		},
		{
			name:    "powershell simple statement",
			script:  "Write-Host hello",
			variant: VariantPowerShell,
			want:    1,
		},
		{
			name:    "powershell mixed flow control statements are counted individually",
			script:  "Write-Host one; exit 1; Write-Host two",
			variant: VariantPowerShell,
			want:    3,
		},
		{
			name:    "powershell bare exit counts as one statement",
			script:  "exit 1",
			variant: VariantPowerShell,
			want:    1,
		},
		{
			name:    "powershell bare throw counts as one statement",
			script:  `throw "boom"`,
			variant: VariantPowerShell,
			want:    1,
		},
		{
			name:    "cmd chain counts commands",
			script:  "echo hello && echo world",
			variant: VariantCmd,
			want:    2,
		},
		{
			name:    "cmd or chain stays unsplit",
			script:  "echo hello || echo world",
			variant: VariantCmd,
			want:    1,
		},
		{
			name:    "cmd single quotes do not suppress chain splitting",
			script:  "echo 'a && b' && echo done",
			variant: VariantCmd,
			want:    3,
		},
		{
			name:    "cmd caret escaped ampersand does not count as pure and-and chain",
			script:  "echo foo ^&& echo bar && echo baz",
			variant: VariantCmd,
			want:    1,
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
			name:    "or-exit guard stays attached to last command",
			script:  "cmd1 && cmd2 || exit",
			variant: VariantBash,
			want:    []string{"cmd1", "cmd2 || exit"},
		},
		{
			name:    "powershell extracts statements",
			script:  "Write-Host hello; Remove-Item C:\\temp\\foo",
			variant: VariantPowerShell,
			want:    []string{"Write-Host hello", "Remove-Item C:\\temp\\foo"},
		},
		{
			name:    "cmd extracts chained commands",
			script:  "echo hello && del file.txt",
			variant: VariantCmd,
			want:    []string{"echo hello", "del file.txt"},
		},
		{
			name:    "cmd caret escaped ampersand is not treated as pure and-and chain",
			script:  "echo foo ^&& echo bar && echo baz",
			variant: VariantCmd,
			want:    nil,
		},
		{
			name:    "cmd single quotes stay literal",
			script:  "echo 'a && b' && echo done",
			variant: VariantCmd,
			want:    []string{"echo 'a", "b'", "echo done"},
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

func TestExtractCmdStatementsFromAnalysis(t *testing.T) {
	t.Parallel()

	t.Run("accepts valid parser-aligned spans", func(t *testing.T) {
		t.Parallel()

		script := "echo hello && echo world"
		analysis := &CmdScriptAnalysis{
			Commands:          []CommandInfo{{Name: "echo"}, {Name: "echo"}},
			commandByteRanges: [][2]uint{{0, 10}, {14, 24}},
			conditionalOps:    []cmdConditionalOp{{Text: "&&", Start: 11, End: 13}},
		}

		got, ok := extractCmdStatementsFromAnalysis(script, analysis)
		if !ok {
			t.Fatal("expected extraction to succeed")
		}
		want := []string{"echo hello", "echo world"}
		if len(got) != len(want) {
			t.Fatalf("returned %d commands, want %d: %v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("command %d = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("rejects out of order command ranges", func(t *testing.T) {
		t.Parallel()

		script := "echo hello && echo world"
		analysis := &CmdScriptAnalysis{
			Commands:          []CommandInfo{{Name: "echo"}, {Name: "echo"}},
			commandByteRanges: [][2]uint{{0, 10}, {9, 24}},
			conditionalOps:    []cmdConditionalOp{{Text: "&&", Start: 11, End: 13}},
		}

		got, ok := extractCmdStatementsFromAnalysis(script, analysis)
		if ok || got != nil {
			t.Fatalf("expected malformed ranges to be rejected, got ok=%v parts=%v", ok, got)
		}
	})
}

func TestExtractChainSeparators(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		script       string
		variant      Variant
		commandCount int
		want         []string
	}{
		{
			name:         "single-line separators",
			script:       "apt-get update && apt-get install -y curl && apt-get clean",
			variant:      VariantBash,
			commandCount: 3,
			want:         []string{" && ", " && "},
		},
		{
			name: "continuation separators",
			script: "apt-get update && \\\n" +
				"    apt-get install -y curl && \\\n" +
				"    apt-get clean",
			variant:      VariantBash,
			commandCount: 3,
			want:         []string{" && \\\n    ", " && \\\n    "},
		},
		{
			name:         "mismatched command count returns nil",
			script:       "apt-get update && apt-get install -y curl",
			variant:      VariantBash,
			commandCount: 3,
			want:         nil,
		},
		{
			name:         "non-posix returns nil",
			script:       "apt-get update && apt-get install -y curl",
			variant:      VariantPowerShell,
			commandCount: 2,
			want:         nil,
		},
		{
			name:         "semicolon separators",
			script:       "apt-get update; apt-get install -y curl; apt-get clean",
			variant:      VariantBash,
			commandCount: 3,
			want:         []string{"; ", "; "},
		},
		{
			name: "semicolon with continuation separators",
			script: "apt-get update; \\\n" +
				"    apt-get install -y curl; \\\n" +
				"    apt-get clean",
			variant:      VariantBash,
			commandCount: 3,
			want:         []string{"; \\\n    ", "; \\\n    "},
		},
		{
			name: "mixed semicolon and && separators",
			script: "set -ex; \\\n" +
				"    apt-get update && apt-get install -y curl; \\\n" +
				"    apt-get clean",
			variant:      VariantBash,
			commandCount: 4,
			want:         []string{"; \\\n    ", " && ", "; \\\n    "},
		},
		{
			name:         "newline separators at top level",
			script:       "apt-get update\napt-get install -y curl\napt-get clean",
			variant:      VariantBash,
			commandCount: 3,
			want:         []string{"\n", "\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractChainSeparators(tt.script, tt.variant, tt.commandCount)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractChainSeparators() returned %d separators, want %d: %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("separator %d = %q, want %q", i, got[i], tt.want[i])
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
			name:    "exit is simple",
			script:  "exit 0",
			variant: VariantBash,
			want:    true,
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
			name:    "powershell simple statements",
			script:  "Write-Host hello; Remove-Item C:\\temp\\foo",
			variant: VariantPowerShell,
			want:    true,
		},
		{
			name:    "powershell flow control is not simple",
			script:  "Write-Host one; exit 1; Write-Host two",
			variant: VariantPowerShell,
			want:    false,
		},
		{
			name:    "cmd simple chained commands",
			script:  "echo hello && del file.txt",
			variant: VariantCmd,
			want:    true,
		},
		{
			name:    "or chain is simple",
			script:  "test -f file || touch file",
			variant: VariantBash,
			want:    true, // || chains stay as single line, set -e doesn't exit on || parts
		},
		{
			name:    "mixed and-or chain is simple",
			script:  "cmd1 && cmd2 || cmd3",
			variant: VariantBash,
			want:    true, // || chains stay as single line in heredoc
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
			name:    "powershell exit is detected",
			script:  "Write-Host hello; exit 1",
			variant: VariantPowerShell,
			want:    true,
		},
		{
			name:    "cmd exit is detected",
			script:  "echo hello && exit /b 1",
			variant: VariantCmd,
			want:    true,
		},
		{
			name:    "cmd prefixed exit is detected",
			script:  "@exit /b 1",
			variant: VariantCmd,
			want:    true,
		},
		{
			name:    "cmd echoed exit is not detected",
			script:  "echo exit",
			variant: VariantCmd,
			want:    false,
		},
		{
			name:    "cmd comment exit is not detected",
			script:  "REM exit",
			variant: VariantCmd,
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

func TestHasPipes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		script  string
		variant Variant
		want    bool
	}{
		{
			name:    "powershell quoted pipe is not a pipeline",
			script:  `Write-Host "a|b"`,
			variant: VariantPowerShell,
			want:    false,
		},
		{
			name:    "powershell pipeline is detected",
			script:  `Get-Content foo.txt | Select-Object -First 1`,
			variant: VariantPowerShell,
			want:    true,
		},
		{
			name:    "powershell multi-stage pipeline",
			script:  `Get-Process | Where-Object CPU -gt 10 | Sort-Object CPU`,
			variant: VariantPowerShell,
			want:    true,
		},
		{
			name:    "powershell single command is not a pipeline",
			script:  `Get-ChildItem -Recurse`,
			variant: VariantPowerShell,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := HasPipes(tt.script, tt.variant)
			if got != tt.want {
				t.Errorf("HasPipes() = %v, want %v", got, tt.want)
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
			name:        "script with exit - candidate",
			script:      "apt-get update && exit 0 && apt-get install vim",
			variant:     VariantBash,
			minCommands: 2,
			want:        true,
		},
		{
			name:        "powershell candidate",
			script:      "Write-Host one; Write-Host two; Write-Host three",
			variant:     VariantPowerShell,
			minCommands: 3,
			want:        true,
		},
		{
			name:        "cmd candidate",
			script:      "echo one && echo two && echo three",
			variant:     VariantCmd,
			minCommands: 3,
			want:        true,
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
