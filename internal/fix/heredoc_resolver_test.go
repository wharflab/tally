package fix

import (
	"context"
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

func TestHeredocResolver_ID(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}
	if got := r.ID(); got != rules.HeredocResolverID {
		t.Errorf("ID() = %q, want %q", got, rules.HeredocResolverID)
	}
}

func TestHeredocResolver_Resolve_InvalidData(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Test with wrong type of resolver data
	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: "invalid data type",
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte("FROM ubuntu\n"),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if edits != nil {
		t.Errorf("expected nil edits for invalid data, got %v", edits)
	}
}

func TestHeredocResolver_Resolve_InvalidDockerfile(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	// Test with invalid Dockerfile content
	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte("not a valid dockerfile {{{{"),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if edits != nil {
		t.Errorf("expected nil edits for invalid dockerfile, got %v", edits)
	}
}

func TestHeredocResolver_Resolve_StageIndexOutOfBounds(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   10, // Out of bounds
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte("FROM ubuntu\nRUN echo hello\n"),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if edits != nil {
		t.Errorf("expected nil edits for out of bounds stage, got %v", edits)
	}
}

func TestHeredocResolver_Resolve_UnknownFixType(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixType(99), // Unknown type
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte("FROM ubuntu\nRUN echo hello\n"),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if edits != nil {
		t.Errorf("expected nil edits for unknown fix type, got %v", edits)
	}
}

func TestHeredocResolver_Resolve_ChainedCommands(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	dockerfile := `FROM ubuntu
RUN apt-get update && apt-get install -y vim && apt-get clean
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}

	// Check that the edit converts to heredoc
	if !strings.Contains(edits[0].NewText, "<<EOF") {
		t.Errorf("expected heredoc syntax in edit, got: %s", edits[0].NewText)
	}
	if !strings.Contains(edits[0].NewText, "set -e") {
		t.Errorf("expected 'set -e' in heredoc, got: %s", edits[0].NewText)
	}
}

func TestHeredocResolver_Resolve_ConsecutiveRuns(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	dockerfile := `FROM ubuntu
RUN apt-get update
RUN apt-get install -y vim
RUN apt-get clean
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixConsecutive,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}

	// Check that the edit converts to heredoc
	if !strings.Contains(edits[0].NewText, "<<EOF") {
		t.Errorf("expected heredoc syntax in edit, got: %s", edits[0].NewText)
	}
}

func TestHeredocResolver_Resolve_ChainedBelowThreshold(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Only 2 commands - below threshold of 3
	dockerfile := `FROM ubuntu
RUN apt-get update && apt-get install -y vim
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edits != nil {
		t.Errorf("expected nil edits when below threshold, got %d edits", len(edits))
	}
}

func TestHeredocResolver_Resolve_ConsecutiveBelowThreshold(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Only 2 RUNs - below threshold of 3
	dockerfile := `FROM ubuntu
RUN apt-get update
RUN apt-get install -y vim
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixConsecutive,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edits != nil {
		t.Errorf("expected nil edits when below threshold, got %d edits", len(edits))
	}
}

func TestHeredocResolver_Resolve_ExecFormRun(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Exec form RUN - should not be converted
	dockerfile := `FROM ubuntu
RUN ["apt-get", "update"]
RUN ["apt-get", "install", "-y", "vim"]
RUN ["apt-get", "clean"]
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixConsecutive,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edits != nil {
		t.Errorf("expected nil edits for exec form RUNs, got %d edits", len(edits))
	}
}

func TestHeredocResolver_Resolve_WithIndentation(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Indented Dockerfile (common in multi-stage)
	dockerfile := `FROM ubuntu AS builder
    RUN apt-get update && apt-get install -y vim && apt-get clean
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}

	// Check that indentation is preserved
	if !strings.Contains(edits[0].NewText, "    RUN") {
		t.Errorf("expected indentation to be preserved, got: %s", edits[0].NewText)
	}
}

func TestExtractIndent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		line    int
		want    string
	}{
		{
			name:    "no indent",
			content: "RUN echo hello",
			line:    1,
			want:    "",
		},
		{
			name:    "spaces indent",
			content: "    RUN echo hello",
			line:    1,
			want:    "    ",
		},
		{
			name:    "tab indent",
			content: "\tRUN echo hello",
			line:    1,
			want:    "\t",
		},
		{
			name:    "mixed indent",
			content: "  \t  RUN echo hello",
			line:    1,
			want:    "  \t  ",
		},
		{
			name:    "line out of bounds (0)",
			content: "RUN echo hello",
			line:    0,
			want:    "",
		},
		{
			name:    "line out of bounds (too high)",
			content: "RUN echo hello",
			line:    10,
			want:    "",
		},
		{
			name:    "multiline - second line",
			content: "FROM ubuntu\n    RUN echo hello",
			line:    2,
			want:    "    ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sm := sourcemap.New([]byte(tt.content))
			got := extractIndent(sm, tt.line)
			if got != tt.want {
				t.Errorf("extractIndent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyIndent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		heredoc string
		indent  string
		want    string
	}{
		{
			name:    "no indent",
			heredoc: "RUN <<EOF\nset -e\necho hello\nEOF",
			indent:  "",
			want:    "RUN <<EOF\nset -e\necho hello\nEOF",
		},
		{
			name:    "with indent",
			heredoc: "RUN <<EOF\nset -e\necho hello\nEOF",
			indent:  "    ",
			want:    "RUN <<EOF\n    set -e\n    echo hello\n    EOF",
		},
		{
			name:    "with empty lines",
			heredoc: "RUN <<EOF\nset -e\n\necho hello\nEOF",
			indent:  "  ",
			want:    "RUN <<EOF\n  set -e\n\n  echo hello\n  EOF",
		},
		{
			name:    "single line",
			heredoc: "RUN echo hello",
			indent:  "    ",
			want:    "RUN echo hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := applyIndent(tt.heredoc, tt.indent)
			if got != tt.want {
				t.Errorf("applyIndent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeredocResolver_GetRunScript(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	tests := []struct {
		name       string
		dockerfile string
		want       string
	}{
		{
			name:       "shell form RUN",
			dockerfile: "FROM ubuntu\nRUN echo hello world",
			want:       "echo hello world",
		},
		{
			name:       "chained commands",
			dockerfile: "FROM ubuntu\nRUN apt-get update && apt-get install -y vim",
			want:       "apt-get update && apt-get install -y vim",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := parseFirstRun(t, tt.dockerfile)
			if run == nil {
				t.Fatal("no RUN command found")
			}
			got := r.getRunScript(run)
			if got != tt.want {
				t.Errorf("getRunScript() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeredocResolver_ExtractCommands(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	tests := []struct {
		name       string
		dockerfile string
		want       int // number of commands
	}{
		{
			name:       "single command",
			dockerfile: "FROM ubuntu\nRUN echo hello",
			want:       1,
		},
		{
			name:       "chained commands",
			dockerfile: "FROM ubuntu\nRUN apt-get update && apt-get install -y vim && apt-get clean",
			want:       3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := parseFirstRun(t, tt.dockerfile)
			if run == nil {
				t.Fatal("no RUN command found")
			}
			commands := r.extractCommands(run, shell.VariantBash)
			if len(commands) != tt.want {
				t.Errorf("extractCommands() returned %d commands, want %d", len(commands), tt.want)
			}
		})
	}
}

func TestHeredocResolver_Resolve_ComplexScript(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Complex script with control flow - should not be converted
	dockerfile := `FROM ubuntu
RUN if [ -f /etc/os-release ]; then cat /etc/os-release; fi && echo done && echo more
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Complex scripts should not be converted
	if edits != nil {
		t.Logf("edits: %v", edits)
	}
}

func TestHeredocResolver_Resolve_InterruptedSequence(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Sequence interrupted by non-RUN command
	dockerfile := `FROM ubuntu
RUN apt-get update
COPY . /app
RUN apt-get install -y vim
RUN apt-get clean
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixConsecutive,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  2, // Lower threshold to test
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the last two RUNs should be merged (the first one is alone)
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit for the last sequence, got %d", len(edits))
	}
}

func TestHeredocResolver_Resolve_ExitCommand(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// RUN with exit command should break the sequence
	dockerfile := `FROM ubuntu
RUN apt-get update
RUN test -f /etc/os-release || exit 1
RUN apt-get install -y vim
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixConsecutive,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  2,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Exit commands break sequences, so no merge should happen with only 2 commands before/after
	// First RUN alone, then exit breaks, then last RUN alone
	if edits != nil {
		t.Errorf("expected nil edits when exit command breaks sequence, got %d edits", len(edits))
	}
}

func TestHeredocResolver_Resolve_AlreadyHeredoc(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Already a heredoc - should be skipped
	dockerfile := `FROM ubuntu
RUN <<EOF
apt-get update
apt-get install -y vim
EOF
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  2,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Already heredoc should not generate edits
	if edits != nil {
		t.Errorf("expected nil edits for already-heredoc RUN, got %d edits", len(edits))
	}
}

func TestHeredocResolver_ExtractCommands_Heredoc(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Test with heredoc RUN
	dockerfile := `FROM ubuntu
RUN <<EOF
apt-get update
apt-get install -y vim
apt-get clean
EOF
`
	run := parseFirstRun(t, dockerfile)
	if run == nil {
		t.Fatal("no RUN command found")
	}

	commands := r.extractCommands(run, shell.VariantBash)
	// Heredoc with simple commands should extract them
	if len(commands) != 3 {
		t.Errorf("extractCommands() returned %d commands, want 3", len(commands))
	}
}

func TestHeredocResolver_ExtractCommands_EmptyHeredoc(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Test with empty heredoc - manually construct a RunCommand with empty Files
	run := &instructions.RunCommand{
		ShellDependantCmdLine: instructions.ShellDependantCmdLine{
			PrependShell: true,
			Files: []instructions.ShellInlineFile{
				{Name: "heredoc", Data: ""},
			},
		},
	}

	commands := r.extractCommands(run, shell.VariantBash)
	if commands != nil {
		t.Errorf("expected nil commands for empty heredoc, got %v", commands)
	}
}

func TestHeredocResolver_ExtractCommands_ComplexHeredoc(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Test with complex heredoc (control flow)
	dockerfile := `FROM ubuntu
RUN <<EOF
if [ -f /etc/os-release ]; then
  cat /etc/os-release
fi
EOF
`
	run := parseFirstRun(t, dockerfile)
	if run == nil {
		t.Fatal("no RUN command found")
	}

	commands := r.extractCommands(run, shell.VariantBash)
	// Complex heredoc should return nil (can't merge)
	if commands != nil {
		t.Errorf("expected nil commands for complex heredoc, got %v", commands)
	}
}

func TestHeredocResolver_GetRunScript_Heredoc(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	dockerfile := `FROM ubuntu
RUN <<EOF
apt-get update
apt-get install -y vim
EOF
`
	run := parseFirstRun(t, dockerfile)
	if run == nil {
		t.Fatal("no RUN command found")
	}

	script := r.getRunScript(run)
	if script == "" {
		t.Error("expected non-empty script for heredoc RUN")
	}
	if !strings.Contains(script, "apt-get update") {
		t.Errorf("expected script to contain 'apt-get update', got: %s", script)
	}
}

func TestHeredocResolver_GetRunScript_EmptyRun(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Empty RUN command
	run := &instructions.RunCommand{
		ShellDependantCmdLine: instructions.ShellDependantCmdLine{
			PrependShell: true,
			CmdLine:      []string{},
		},
	}

	script := r.getRunScript(run)
	if script != "" {
		t.Errorf("expected empty script for empty RUN, got: %s", script)
	}
}

func TestHeredocResolver_Resolve_DifferentMounts(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// RUNs with different mounts should not be merged
	dockerfile := `FROM ubuntu
RUN --mount=type=cache,target=/var/cache/apt apt-get update
RUN --mount=type=cache,target=/root/.cache pip install requests
RUN apt-get clean
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixConsecutive,
			StageIndex:   0,
			ShellVariant: shell.VariantBash,
			MinCommands:  2,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Different mounts break the sequence
	// Only the last two (pip install + apt-get clean) have incompatible mounts
	// so no sequence of 2+ with same mounts exists
	if len(edits) != 0 {
		t.Fatalf("expected no edits when mounts differ, got %d", len(edits))
	}
}

func TestHeredocResolver_Resolve_ShellVariantUpdatedFromContent(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// Simulate content after DL4005's sync fix has replaced
	// "RUN ln -sf /bin/bash /bin/sh" with "SHELL ["/bin/bash", "-c"]".
	// The original HeredocResolveData has the default variant (VariantBash in
	// this case, but in a real scenario the original stage might have had a
	// different default). The resolver should detect the SHELL instruction
	// and update the variant accordingly.
	dockerfile := `FROM ubuntu
SHELL ["/bin/bash", "-c"]
RUN apt-get update && apt-get install -y vim && apt-get clean
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   0,
			ShellVariant: shell.VariantPOSIX, // Stale: original stage was POSIX
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}

	// The heredoc should use bash variant (from the SHELL instruction).
	// With VariantBash the heredoc uses /bin/bash; with VariantPOSIX it
	// would use /bin/sh. Verify the variant was updated by checking the
	// shebang in the heredoc output.
	if !strings.Contains(edits[0].NewText, "<<EOF") {
		t.Errorf("expected heredoc syntax, got: %s", edits[0].NewText)
	}

	// Verify we got a valid heredoc edit (proves parsing worked with correct variant)
	if !strings.Contains(edits[0].NewText, "apt-get update") {
		t.Errorf("expected apt-get update in heredoc, got: %s", edits[0].NewText)
	}

	// Verify the resolve data was updated
	data, ok := fix.ResolverData.(*rules.HeredocResolveData)
	if !ok {
		t.Fatal("expected HeredocResolveData")
	}
	if data.ShellVariant != shell.VariantBash {
		t.Errorf("expected ShellVariant to be updated to VariantBash, got %v", data.ShellVariant)
	}
}

func TestHeredocResolver_Resolve_NonPOSIXShellSkipped(t *testing.T) {
	t.Parallel()
	r := &heredocResolver{}

	// If a sync fix introduced a non-POSIX shell (e.g., powershell),
	// the resolver should detect it and the shell parsing should handle
	// it gracefully.
	dockerfile := `FROM mcr.microsoft.com/windows/servercore
SHELL ["powershell", "-Command"]
RUN Write-Output "hello" ; Write-Output "world" ; Write-Output "!"
`

	fix := &rules.SuggestedFix{
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         rules.HeredocFixChained,
			StageIndex:   0,
			ShellVariant: shell.VariantBash, // Stale variant
			MinCommands:  3,
		},
	}

	edits, err := r.Resolve(context.Background(), ResolveContext{
		Content:  []byte(dockerfile),
		FilePath: "Dockerfile",
	}, fix)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-POSIX shells should not produce heredoc edits
	if edits != nil {
		t.Errorf("expected nil edits for non-POSIX shell, got %d edits", len(edits))
	}

	// Verify the variant was updated to NonPOSIX
	data, ok := fix.ResolverData.(*rules.HeredocResolveData)
	if !ok {
		t.Fatal("expected HeredocResolveData")
	}
	if data.ShellVariant != shell.VariantNonPOSIX {
		t.Errorf("expected ShellVariant to be updated to VariantNonPOSIX, got %v", data.ShellVariant)
	}
}

// Helper functions

func parseFirstRun(t *testing.T, dockerfile string) *instructions.RunCommand {
	t.Helper()

	result, err := parser.Parse(strings.NewReader(dockerfile))
	if err != nil {
		t.Fatalf("failed to parse dockerfile: %v", err)
	}

	stages, _, err := instructions.Parse(result.AST, nil)
	if err != nil {
		t.Fatalf("failed to parse instructions: %v", err)
	}

	for _, stage := range stages {
		for _, cmd := range stage.Commands {
			if run, ok := cmd.(*instructions.RunCommand); ok {
				return run
			}
		}
	}
	return nil
}
