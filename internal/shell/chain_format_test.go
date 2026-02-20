package shell

import (
	"testing"
)

func TestCollectChainBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		script         string
		variant        Variant
		wantBoundaries int
		wantCmds       int
		wantSameLine   []bool // per-boundary SameLine values
		wantOps        []string
	}{
		{
			name:           "single command",
			script:         "echo hello",
			variant:        VariantBash,
			wantBoundaries: 0,
			wantCmds:       1,
		},
		{
			name:           "two commands same line",
			script:         "apt-get update && apt-get install -y curl",
			variant:        VariantBash,
			wantBoundaries: 1,
			wantCmds:       2,
			wantSameLine:   []bool{true},
			wantOps:        []string{"&&"},
		},
		{
			name:           "three commands same line",
			script:         "cmd1 && cmd2 && cmd3",
			variant:        VariantBash,
			wantBoundaries: 2,
			wantCmds:       3,
			wantSameLine:   []bool{true, true},
			wantOps:        []string{"&&", "&&"},
		},
		{
			name:           "or chain",
			script:         "cmd1 || cmd2",
			variant:        VariantBash,
			wantBoundaries: 1,
			wantCmds:       2,
			wantSameLine:   []bool{true},
			wantOps:        []string{"||"},
		},
		{
			name:           "mixed and-or chain",
			script:         "cmd1 && cmd2 || cmd3",
			variant:        VariantBash,
			wantBoundaries: 2,
			wantCmds:       3,
			wantSameLine:   []bool{true, true},
			wantOps:        []string{"&&", "||"},
		},
		{
			name:           "already split across lines",
			script:         "cmd1 \\\n\t&& cmd2",
			variant:        VariantBash,
			wantBoundaries: 1,
			wantCmds:       2,
			wantSameLine:   []bool{false},
			wantOps:        []string{"&&"},
		},
		{
			name:           "pipe is single command",
			script:         "cat file | grep pattern",
			variant:        VariantBash,
			wantBoundaries: 0,
			wantCmds:       1,
		},
		{
			name:           "semicolon separated statements",
			script:         "echo a; echo b",
			variant:        VariantBash,
			wantBoundaries: 0,
			wantCmds:       2,
		},
		{
			name:           "non-POSIX shell",
			script:         "cmd1 && cmd2",
			variant:        VariantNonPOSIX,
			wantBoundaries: 0,
			wantCmds:       0,
		},
		{
			name:           "empty script",
			script:         "",
			variant:        VariantBash,
			wantBoundaries: 0,
			wantCmds:       0,
		},
		{
			name:           "mixed split and unsplit",
			script:         "cmd1 \\\n\t&& cmd2 && cmd3",
			variant:        VariantBash,
			wantBoundaries: 2,
			wantCmds:       3,
			wantSameLine:   []bool{false, true},
			wantOps:        []string{"&&", "&&"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			boundaries, cmds := CollectChainBoundaries(tt.script, tt.variant)

			if cmds != tt.wantCmds {
				t.Errorf("cmds = %d, want %d", cmds, tt.wantCmds)
			}
			if len(boundaries) != tt.wantBoundaries {
				t.Errorf("boundaries = %d, want %d", len(boundaries), tt.wantBoundaries)
			}

			for i, b := range boundaries {
				if i < len(tt.wantSameLine) && b.SameLine != tt.wantSameLine[i] {
					t.Errorf("boundary[%d].SameLine = %v, want %v", i, b.SameLine, tt.wantSameLine[i])
				}
				if i < len(tt.wantOps) && b.Op != tt.wantOps[i] {
					t.Errorf("boundary[%d].Op = %q, want %q", i, b.Op, tt.wantOps[i])
				}
			}
		})
	}
}

func TestScriptHasInlineHeredoc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		script  string
		variant Variant
		want    bool
	}{
		{
			name:    "no heredoc",
			script:  "echo hello && echo world",
			variant: VariantBash,
			want:    false,
		},
		{
			name:    "inline heredoc",
			script:  "cat <<EOF\nhello\nEOF",
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "inline heredoc in chain",
			script:  "cat <<EOF && echo done\nhello\nEOF",
			variant: VariantBash,
			want:    true,
		},
		{
			name:    "non-POSIX",
			script:  "cat <<EOF\nhello\nEOF",
			variant: VariantNonPOSIX,
			want:    false,
		},
		{
			name:    "empty script",
			script:  "",
			variant: VariantBash,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ScriptHasInlineHeredoc(tt.script, tt.variant)
			if got != tt.want {
				t.Errorf("ScriptHasInlineHeredoc() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatChainedScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		script  string
		variant Variant
		want    string
	}{
		{
			name:    "single command unchanged",
			script:  "echo hello",
			variant: VariantBash,
			want:    "echo hello",
		},
		{
			name:    "two commands",
			script:  "apt-get update && apt-get install -y curl",
			variant: VariantBash,
			want:    "apt-get update \\\n\t&& apt-get install -y curl",
		},
		{
			name:    "three commands mixed operators",
			script:  "cmd1 && cmd2 || cmd3",
			variant: VariantBash,
			want:    "cmd1 \\\n\t&& cmd2 \\\n\t|| cmd3",
		},
		{
			name:    "pipe preserved as single command",
			script:  "cmd1 | cmd2 && cmd3",
			variant: VariantBash,
			want:    "cmd1 | cmd2 \\\n\t&& cmd3",
		},
		{
			name:    "four chained commands",
			script:  "a && b && c && d",
			variant: VariantBash,
			want:    "a \\\n\t&& b \\\n\t&& c \\\n\t&& d",
		},
		{
			name:    "non-POSIX returns trimmed input",
			script:  "cmd1 && cmd2",
			variant: VariantNonPOSIX,
			want:    "cmd1 && cmd2",
		},
		{
			name:    "no chains returns trimmed input",
			script:  "  echo hello  ",
			variant: VariantBash,
			want:    "echo hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormatChainedScript(tt.script, tt.variant)
			if got != tt.want {
				t.Errorf("FormatChainedScript():\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestReconstructSourceText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		lines       []string
		cmdStartCol int
		want        string
	}{
		{
			name:        "single line",
			lines:       []string{"RUN echo hello"},
			cmdStartCol: 4,
			want:        "echo hello",
		},
		{
			name:        "multi-line with continuation",
			lines:       []string{`RUN echo hello \`, `    && echo world`},
			cmdStartCol: 4,
			want:        "echo hello \\\n    && echo world",
		},
		{
			name:        "with mount prefix",
			lines:       []string{"RUN --mount=type=cache,target=/var apt-get update"},
			cmdStartCol: 35,
			want:        "apt-get update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ReconstructSourceText(tt.lines, tt.cmdStartCol)
			if got != tt.want {
				t.Errorf("ReconstructSourceText():\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}
