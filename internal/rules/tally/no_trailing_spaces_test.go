package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoTrailingSpacesMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoTrailingSpacesRule().Metadata())
}

func TestNoTrailingSpacesDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := NewNoTrailingSpacesRule().DefaultConfig()
	got, ok := cfg.(NoTrailingSpacesConfig)
	if !ok {
		t.Fatalf("DefaultConfig() type = %T, want NoTrailingSpacesConfig", cfg)
	}
	if got.SkipBlankLines == nil || *got.SkipBlankLines {
		t.Errorf("SkipBlankLines = %v, want false", got.SkipBlankLines)
	}
	if got.IgnoreComments == nil || *got.IgnoreComments {
		t.Errorf("IgnoreComments = %v, want false", got.IgnoreComments)
	}
}

func TestNoTrailingSpacesValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewNoTrailingSpacesRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{name: "nil config", config: nil, wantErr: false},
		{name: "empty object", config: map[string]any{}, wantErr: false},
		{name: "skip-blank-lines true", config: map[string]any{"skip-blank-lines": true}, wantErr: false},
		{name: "ignore-comments true", config: map[string]any{"ignore-comments": true}, wantErr: false},
		{name: "both options", config: map[string]any{"skip-blank-lines": true, "ignore-comments": true}, wantErr: false},
		{name: "extra key", config: map[string]any{"unknown": true}, wantErr: true},
		{name: "wrong type", config: map[string]any{"skip-blank-lines": "yes"}, wantErr: true},
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

func TestNoTrailingSpacesCheck(t *testing.T) {
	t.Parallel()

	boolTrue := func() *bool { b := true; return &b }

	testutil.RunRuleTests(t, NewNoTrailingSpacesRule(), []testutil.RuleTestCase{
		// === Clean files ===
		{
			Name:           "clean file - no trailing whitespace",
			Content:        "FROM alpine:3.20\nRUN echo hello\nCOPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "empty file",
			Content:        "FROM scratch\n",
			WantViolations: 0,
		},

		// === Trailing spaces ===
		{
			Name:           "trailing spaces on instruction",
			Content:        "FROM alpine:3.20   \nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"trailing whitespace"},
		},
		{
			Name:           "trailing spaces on multiple lines",
			Content:        "FROM alpine:3.20  \nRUN echo hello   \nCOPY . /app \n",
			WantViolations: 3,
		},

		// === Trailing tabs ===
		{
			Name:           "trailing tabs",
			Content:        "FROM alpine:3.20\t\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"trailing whitespace"},
		},

		// === Mixed trailing whitespace ===
		{
			Name:           "mixed trailing spaces and tabs",
			Content:        "FROM alpine:3.20 \t \nRUN echo hello\n",
			WantViolations: 1,
		},

		// === Blank lines with only whitespace ===
		{
			Name:           "blank line with spaces - violation by default",
			Content:        "FROM alpine:3.20\n   \nRUN echo hello\n",
			WantViolations: 1,
		},
		{
			Name:    "blank line with spaces - skipped with skip-blank-lines",
			Content: "FROM alpine:3.20\n   \nRUN echo hello\n",
			Config: NoTrailingSpacesConfig{
				SkipBlankLines: boolTrue(),
			},
			WantViolations: 0,
		},
		{
			Name:    "blank line with tabs - skipped with skip-blank-lines",
			Content: "FROM alpine:3.20\n\t\t\nRUN echo hello\n",
			Config: NoTrailingSpacesConfig{
				SkipBlankLines: boolTrue(),
			},
			WantViolations: 0,
		},
		{
			Name:    "skip-blank-lines still flags non-blank lines",
			Content: "FROM alpine:3.20  \n   \nRUN echo hello\n",
			Config: NoTrailingSpacesConfig{
				SkipBlankLines: boolTrue(),
			},
			WantViolations: 1, // only FROM line flagged
			WantMessages:   []string{"trailing whitespace"},
		},

		// === Comment lines ===
		{
			Name:           "comment line with trailing spaces - violation by default",
			Content:        "# build image   \nFROM alpine:3.20\n",
			WantViolations: 1,
		},
		{
			Name:    "comment line with trailing spaces - skipped with ignore-comments",
			Content: "# build image   \nFROM alpine:3.20\n",
			Config: NoTrailingSpacesConfig{
				IgnoreComments: boolTrue(),
			},
			WantViolations: 0,
		},
		{
			Name:    "indented comment with trailing spaces - skipped with ignore-comments",
			Content: "FROM alpine:3.20\n  # a comment   \nRUN echo hello\n",
			Config: NoTrailingSpacesConfig{
				IgnoreComments: boolTrue(),
			},
			WantViolations: 0,
		},
		{
			Name:    "ignore-comments still flags non-comment lines",
			Content: "# build image   \nFROM alpine:3.20  \n",
			Config: NoTrailingSpacesConfig{
				IgnoreComments: boolTrue(),
			},
			WantViolations: 1, // only FROM line flagged
		},

		// === Continuation lines ===
		{
			Name:           "whitespace after backslash continuation",
			Content:        "FROM alpine:3.20\nRUN echo hello \\   \n    && echo world\n",
			WantViolations: 1,
			WantMessages:   []string{"trailing whitespace"},
		},

		// === Heredoc body ===
		{
			Name:           "heredoc body with trailing whitespace",
			Content:        "FROM alpine:3.20\nRUN <<EOF\necho hello   \nEOF\n",
			WantViolations: 1,
		},
		{
			Name:    "ignore-comments also skips hash lines inside heredoc bodies",
			Content: "FROM alpine:3.20\nRUN <<EOF\n# shell comment   \necho hello\nEOF\n",
			Config: NoTrailingSpacesConfig{
				IgnoreComments: boolTrue(),
			},
			WantViolations: 0, // line-based check; # inside heredoc is also skipped
		},

		// === Both options combined ===
		{
			Name:    "both options skip blank and comment lines",
			Content: "# comment   \nFROM alpine:3.20\n   \nRUN echo hello  \n",
			Config: NoTrailingSpacesConfig{
				SkipBlankLines: boolTrue(),
				IgnoreComments: boolTrue(),
			},
			WantViolations: 1, // only RUN line flagged
		},
	})
}

func TestNoTrailingSpacesCheckWithFixes(t *testing.T) {
	t.Parallel()
	r := NewNoTrailingSpacesRule()

	tests := []struct {
		name      string
		content   string
		wantEdits int
	}{
		{
			name:      "single trailing space",
			content:   "FROM alpine:3.20 \nRUN echo hello\n",
			wantEdits: 1,
		},
		{
			name:      "multiple lines with trailing whitespace",
			content:   "FROM alpine:3.20  \nRUN echo hello\t\nCOPY . /app   \n",
			wantEdits: 3,
		},
		{
			name:      "blank line with whitespace",
			content:   "FROM alpine:3.20\n   \nRUN echo hello\n",
			wantEdits: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, nil)
			violations := r.Check(input)

			totalEdits := 0
			for _, v := range violations {
				if v.SuggestedFix == nil {
					t.Error("violation has no SuggestedFix")
					continue
				}
				if v.SuggestedFix.Safety != rules.FixSafe {
					t.Errorf("fix safety = %v, want FixSafe", v.SuggestedFix.Safety)
				}
				if v.SuggestedFix.NeedsResolve {
					t.Error("expected NeedsResolve=false for sync fix")
				}
				totalEdits += len(v.SuggestedFix.Edits)
			}

			if totalEdits != tt.wantEdits {
				t.Errorf("total edits = %d, want %d", totalEdits, tt.wantEdits)
			}
		})
	}
}
