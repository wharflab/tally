package tally

import (
	"slices"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
	"go.bug.st/lsp"
	"go.bug.st/lsp/textedits"
)

func TestNewlinePerChainedCallMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNewlinePerChainedCallRule().Metadata())
}

func TestNewlinePerChainedCallDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := NewNewlinePerChainedCallRule().DefaultConfig()
	got, ok := cfg.(NewlinePerChainedCallConfig)
	if !ok {
		t.Fatalf("DefaultConfig() type = %T, want NewlinePerChainedCallConfig", cfg)
	}
	if got.MinCommands == nil || *got.MinCommands != 2 {
		t.Errorf("MinCommands = %v, want 2", got.MinCommands)
	}
}

func TestNewlinePerChainedCallValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewNewlinePerChainedCallRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{name: "nil config", config: nil, wantErr: false},
		{name: "empty object", config: map[string]any{}, wantErr: false},
		{name: "min-commands 3", config: map[string]any{"min-commands": 3}, wantErr: false},
		{name: "min-commands too low", config: map[string]any{"min-commands": 1}, wantErr: true},
		{name: "extra key", config: map[string]any{"unknown": true}, wantErr: true},
		{name: "wrong type", config: map[string]any{"min-commands": "two"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := r.ValidateConfig(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewlinePerChainedCallCheck(t *testing.T) {
	t.Parallel()

	minCommands3 := 3

	testutil.RunRuleTests(t, NewNewlinePerChainedCallRule(), []testutil.RuleTestCase{
		// === RUN chains ===
		{
			Name:           "RUN single command - skip",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "RUN two commands same line - violation",
			Content:        "FROM alpine:3.20\nRUN apt-get update && apt-get install -y curl\n",
			WantViolations: 1,
			WantMessages:   []string{"chained commands"},
		},
		{
			Name:           "RUN three commands same line",
			Content:        "FROM alpine:3.20\nRUN cmd1 && cmd2 && cmd3\n",
			WantViolations: 1,
		},
		{
			Name:           "RUN already split - skip",
			Content:        "FROM alpine:3.20\nRUN apt-get update \\\n\t&& apt-get install -y curl\n",
			WantViolations: 0,
		},
		{
			Name:           "RUN mixed split and unsplit",
			Content:        "FROM alpine:3.20\nRUN cmd1 \\\n\t&& cmd2 && cmd3\n",
			WantViolations: 1,
		},
		{
			Name:           "RUN or chain - violation",
			Content:        "FROM alpine:3.20\nRUN cmd1 || cmd2\n",
			WantViolations: 1,
			WantMessages:   []string{"chained commands"},
		},
		{
			Name:           "RUN mixed and-or",
			Content:        "FROM alpine:3.20\nRUN cmd1 && cmd2 || cmd3\n",
			WantViolations: 1,
		},
		{
			Name:           "RUN heredoc - skip chains",
			Content:        "FROM alpine:3.20\nRUN <<EOF\napt-get update && apt-get install -y curl\nEOF\n",
			WantViolations: 0,
		},
		{
			Name:           "RUN exec form - skip",
			Content:        "FROM alpine:3.20\nRUN [\"echo\", \"hello\"]\n",
			WantViolations: 0,
		},
		{
			Name:    "RUN min-commands=3 skips 2 commands",
			Content: "FROM alpine:3.20\nRUN cmd1 && cmd2\n",
			Config: NewlinePerChainedCallConfig{
				MinCommands: &minCommands3,
			},
			WantViolations: 0,
		},
		{
			Name:    "RUN min-commands=3 triggers on 3 commands",
			Content: "FROM alpine:3.20\nRUN cmd1 && cmd2 && cmd3\n",
			Config: NewlinePerChainedCallConfig{
				MinCommands: &minCommands3,
			},
			WantViolations: 1,
		},
		{
			Name:           "RUN pipe - skip",
			Content:        "FROM alpine:3.20\nRUN cat file | grep pattern\n",
			WantViolations: 0,
		},

		// === RUN mounts ===
		{
			Name:           "RUN single mount - skip",
			Content:        "FROM alpine:3.20\nRUN --mount=type=cache,target=/var/cache/apt apt-get update\n",
			WantViolations: 0,
		},
		{
			Name: "RUN two mounts same line - violation",
			Content: "FROM alpine:3.20\n" +
				"RUN --mount=type=cache,target=/var/cache/apt " +
				"--mount=type=bind,source=go.sum,target=go.sum apt-get update\n",
			WantViolations: 1,
			WantMessages:   []string{"mount flags"},
		},
		{
			Name: "RUN mounts already split - skip",
			Content: "FROM alpine:3.20\n" +
				"RUN --mount=type=cache,target=/var/cache/apt \\\n" +
				"\t--mount=type=bind,source=go.sum,target=go.sum \\\n" +
				"\tapt-get update\n",
			WantViolations: 0,
		},
		{
			Name: "RUN heredoc with multiple mounts - violation on mounts",
			Content: "FROM alpine:3.20\n" +
				"RUN --mount=type=cache,target=/var/cache/apt " +
				"--mount=type=bind,source=go.sum,target=go.sum <<EOF\n" +
				"apt-get update\napt-get install -y curl\nEOF\n",
			WantViolations: 1,
			WantMessages:   []string{"mount flags"},
		},

		// === RUN mounts + chains combined ===
		{
			Name: "RUN mounts and chains combined",
			Content: "FROM alpine:3.20\n" +
				"RUN --mount=type=cache,target=/var/cache/apt " +
				"--mount=type=bind,source=go.sum,target=go.sum " +
				"apt-get update && apt-get install -y curl\n",
			WantViolations: 1,
			WantMessages:   []string{"mount flags and chained commands"},
		},

		// === LABEL ===
		{
			Name:           "LABEL single pair - skip",
			Content:        "FROM alpine:3.20\nLABEL org.opencontainers.image.title=myapp\n",
			WantViolations: 0,
		},
		{
			Name:           "LABEL two pairs same line - violation",
			Content:        "FROM alpine:3.20\nLABEL org.opencontainers.image.title=myapp org.opencontainers.image.version=1.0\n",
			WantViolations: 1,
			WantMessages:   []string{"LABEL"},
		},
		{
			Name:           "LABEL three pairs same line",
			Content:        "FROM alpine:3.20\nLABEL a=1 b=2 c=3\n",
			WantViolations: 1,
		},
		{
			Name:           "LABEL already split - skip",
			Content:        "FROM alpine:3.20\nLABEL org.opencontainers.image.title=myapp \\\n\torg.opencontainers.image.version=1.0\n",
			WantViolations: 0,
		},
		{
			Name: "LABEL many pairs already split - skip",
			Content: "FROM alpine:3.20\n" +
				"LABEL maintainer=\"\" \\\n" +
				"      org.opencontainers.image.created=$BUILD_DATE \\\n" +
				"      org.opencontainers.image.authors=\"httplock maintainers\" \\\n" +
				"      org.opencontainers.image.url=" +
				"\"https://github.com/httplock/httplock\" \\\n" +
				"      org.opencontainers.image.documentation=" +
				"\"https://github.com/httplock/httplock\" \\\n" +
				"      org.opencontainers.image.source=" +
				"\"https://github.com/httplock/httplock\" \\\n" +
				"      org.opencontainers.image.version=\"latest\" \\\n" +
				"      org.opencontainers.image.revision=$VCS_REF \\\n" +
				"      org.opencontainers.image.vendor=\"\" \\\n" +
				"      org.opencontainers.image.licenses=\"Apache 2.0\" \\\n" +
				"      org.opencontainers.image.title=\"httplock\" \\\n" +
				"      org.opencontainers.image.description=\"\"\n",
			WantViolations: 0,
		},
		{
			Name:           "LABEL with quoted value containing spaces",
			Content:        "FROM alpine:3.20\nLABEL maintainer=\"John Doe\" version=1.0\n",
			WantViolations: 1,
			WantMessages:   []string{"LABEL"},
		},
		{
			Name:           "LABEL legacy format - skip",
			Content:        "FROM alpine:3.20\nLABEL maintainer John Doe\n",
			WantViolations: 0,
		},

		// === HEALTHCHECK CMD ===
		{
			Name:           "HEALTHCHECK CMD with chain - violation",
			Content:        "FROM alpine:3.20\nHEALTHCHECK CMD curl -f http://localhost/ && wget -qO- http://localhost/health || exit 1\n",
			WantViolations: 1,
			WantMessages:   []string{"split HEALTHCHECK"},
		},
		{
			Name:           "HEALTHCHECK CMD single command - skip",
			Content:        "FROM alpine:3.20\nHEALTHCHECK CMD curl -f http://localhost/\n",
			WantViolations: 0,
		},
		{
			Name:           "HEALTHCHECK NONE - skip",
			Content:        "FROM alpine:3.20\nHEALTHCHECK NONE\n",
			WantViolations: 0,
		},
		{
			Name:           "HEALTHCHECK exec form - skip",
			Content:        "FROM alpine:3.20\nHEALTHCHECK CMD [\"curl\", \"-f\", \"http://localhost/\"]\n",
			WantViolations: 0,
		},
		{
			Name: "HEALTHCHECK CMD with options and chain",
			Content: "FROM alpine:3.20\n" +
				"HEALTHCHECK --interval=30s --timeout=10s CMD " +
				"curl -f http://localhost/ && wget -qO- http://localhost/health\n",
			WantViolations: 1,
		},
		{
			Name: "HEALTHCHECK flags only no chain - violation",
			Content: "FROM alpine:3.20\n" +
				"HEALTHCHECK --interval=30s --timeout=10s CMD curl -f http://localhost/\n",
			WantViolations: 1,
		},
		{
			Name: "HEALTHCHECK single flag and CMD - violation",
			Content: "FROM alpine:3.20\n" +
				"HEALTHCHECK --interval=30s CMD curl -f http://localhost/\n",
			WantViolations: 1,
		},
		{
			Name: "HEALTHCHECK flags already split - skip",
			Content: "FROM alpine:3.20\n" +
				"HEALTHCHECK --interval=30s \\\n" +
				"\t--timeout=10s \\\n" +
				"\tCMD curl -f http://localhost/\n",
			WantViolations: 0,
		},
	})
}

func TestNewlinePerChainedCallCheckWithFixes(t *testing.T) {
	t.Parallel()
	r := NewNewlinePerChainedCallRule()

	tests := []struct {
		name             string
		content          string
		config           any
		wantEdits        int
		wantFixedContent string // if set, apply edits and compare full result
	}{
		{
			name:      "RUN two commands - one boundary edit",
			content:   "FROM alpine:3.20\nRUN apt-get update && apt-get install -y curl\n",
			wantEdits: 1,
		},
		{
			name:      "RUN three commands - two boundary edits",
			content:   "FROM alpine:3.20\nRUN cmd1 && cmd2 && cmd3\n",
			wantEdits: 2,
		},
		{
			name:      "RUN two mounts - two edits (between mounts + mount-to-cmd)",
			content:   "FROM alpine:3.20\nRUN --mount=type=cache,target=/var --mount=type=bind,source=go.sum,target=go.sum apt-get update\n",
			wantEdits: 2,
		},
		{
			name:      "LABEL two pairs - one edit",
			content:   "FROM alpine:3.20\nLABEL a=1 b=2\n",
			wantEdits: 1,
		},
		{
			name:             "HEALTHCHECK CMD with chain - whole-instruction replacement",
			content:          "FROM alpine:3.20\nHEALTHCHECK CMD cmd1 && cmd2\n",
			wantEdits:        1,
			wantFixedContent: "FROM alpine:3.20\nHEALTHCHECK CMD cmd1 \\\n\t&& cmd2\n",
		},
		{
			name:             "HEALTHCHECK with flags and chain",
			content:          "FROM alpine:3.20\nHEALTHCHECK --interval=30s --timeout=10s CMD cmd1 && cmd2\n",
			wantEdits:        1,
			wantFixedContent: "FROM alpine:3.20\nHEALTHCHECK --interval=30s \\\n\t--timeout=10s \\\n\tCMD cmd1 \\\n\t&& cmd2\n",
		},
		{
			name:             "HEALTHCHECK with flags only - no chain",
			content:          "FROM alpine:3.20\nHEALTHCHECK --interval=30s --timeout=10s CMD curl -f http://localhost/\n",
			wantEdits:        1,
			wantFixedContent: "FROM alpine:3.20\nHEALTHCHECK --interval=30s \\\n\t--timeout=10s \\\n\tCMD curl -f http://localhost/\n",
		},
		{
			name:             "LABEL quoted value with spaces - fix preserves quoting",
			content:          "FROM alpine:3.20\nLABEL maintainer=\"John Doe\" version=1.0\n",
			wantEdits:        1,
			wantFixedContent: "FROM alpine:3.20\nLABEL maintainer=\"John Doe\" \\\n\tversion=1.0\n",
		},
		{
			name: "RUN mount literal in shell command - only splits real mounts",
			content: "FROM alpine:3.20\n" +
				"RUN --mount=type=cache,target=/var/cache/apt " +
				"--mount=type=bind,source=go.sum,target=go.sum " +
				"echo \"--mount=fake\" && echo done\n",
			wantEdits: 3, // 2 mount splits + 1 chain split
			wantFixedContent: "FROM alpine:3.20\n" +
				"RUN --mount=type=cache,target=/var/cache/apt \\\n" +
				"\t--mount=type=bind,source=go.sum,target=go.sum \\\n" +
				"\techo \"--mount=fake\" \\\n" +
				"\t&& echo done\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, tt.config)
			violations := r.Check(input)

			if len(violations) != 1 {
				t.Fatalf("expected 1 violation, got %d", len(violations))
			}

			v := violations[0]
			if v.SuggestedFix == nil {
				t.Fatal("violation has no SuggestedFix")
			}
			if v.SuggestedFix.Safety != rules.FixSafe {
				t.Errorf("fix safety = %v, want FixSafe", v.SuggestedFix.Safety)
			}
			if v.SuggestedFix.NeedsResolve {
				t.Error("expected NeedsResolve=false for sync fix")
			}
			if len(v.SuggestedFix.Edits) != tt.wantEdits {
				t.Errorf("edits = %d, want %d", len(v.SuggestedFix.Edits), tt.wantEdits)
				for i, e := range v.SuggestedFix.Edits {
					t.Logf("  edit[%d]: L%d:%d-L%d:%d → %q", i,
						e.Location.Start.Line, e.Location.Start.Column,
						e.Location.End.Line, e.Location.End.Column,
						e.NewText)
				}
			}

			// Verify continuation lines use tab indentation (not spaces)
			for i, e := range v.SuggestedFix.Edits {
				if strings.Contains(e.NewText, "\n") && !strings.Contains(e.NewText, "\t") {
					t.Errorf("edit[%d]: continuation line missing tab indent: %q", i, e.NewText)
				}
			}

			// If wantFixedContent is set, apply edits and verify final content
			if tt.wantFixedContent != "" {
				got := applyFixEdits(t, tt.content, v.SuggestedFix.Edits)
				if got != tt.wantFixedContent {
					t.Errorf("fixed content mismatch:\n got: %q\nwant: %q", got, tt.wantFixedContent)
				}
			}
		})
	}
}

// applyFixEdits applies rules.TextEdits (1-based lines) to content using the
// go.bug.st/lsp/textedits library. Edits are applied in reverse position order
// to preserve offsets.
func applyFixEdits(t *testing.T, content string, edits []rules.TextEdit) string {
	t.Helper()
	sorted := slices.Clone(edits)
	slices.SortFunc(sorted, func(a, b rules.TextEdit) int {
		if a.Location.Start.Line != b.Location.Start.Line {
			return b.Location.Start.Line - a.Location.Start.Line
		}
		return b.Location.Start.Column - a.Location.Start.Column
	})
	for _, e := range sorted {
		// Convert 1-based line to 0-based for LSP Range
		r := lsp.Range{
			Start: lsp.Position{Line: e.Location.Start.Line - 1, Character: e.Location.Start.Column},
			End:   lsp.Position{Line: e.Location.End.Line - 1, Character: e.Location.End.Column},
		}
		var err error
		content, err = textedits.ApplyTextChange(content, r, e.NewText)
		if err != nil {
			t.Fatalf("ApplyTextChange failed: %v", err)
		}
	}
	return content
}
