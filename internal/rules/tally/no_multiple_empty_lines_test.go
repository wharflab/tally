package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoMultipleEmptyLinesMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoMultipleEmptyLinesRule().Metadata())
}

func TestNoMultipleEmptyLinesDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := NewNoMultipleEmptyLinesRule().DefaultConfig()
	got, ok := cfg.(NoMultipleEmptyLinesConfig)
	if !ok {
		t.Fatalf("DefaultConfig() type = %T, want NoMultipleEmptyLinesConfig", cfg)
	}
	if got.Max == nil || *got.Max != 1 {
		t.Errorf("Max = %v, want 1", got.Max)
	}
	if got.MaxBOF == nil || *got.MaxBOF != 0 {
		t.Errorf("MaxBOF = %v, want 0", got.MaxBOF)
	}
	if got.MaxEOF == nil || *got.MaxEOF != 0 {
		t.Errorf("MaxEOF = %v, want 0", got.MaxEOF)
	}
}

func TestNoMultipleEmptyLinesValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewNoMultipleEmptyLinesRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{name: "nil config", config: nil, wantErr: false},
		{name: "empty object", config: map[string]any{}, wantErr: false},
		{name: "max only", config: map[string]any{"max": 2}, wantErr: false},
		{name: "all options", config: map[string]any{"max": 1, "max-bof": 0, "max-eof": 0}, wantErr: false},
		{name: "max-bof only", config: map[string]any{"max-bof": 1}, wantErr: false},
		{name: "extra key", config: map[string]any{"unknown": true}, wantErr: true},
		{name: "wrong type", config: map[string]any{"max": "yes"}, wantErr: true},
		{name: "negative max", config: map[string]any{"max": -1}, wantErr: true},
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

func TestNoMultipleEmptyLinesCheck(t *testing.T) {
	t.Parallel()

	intPtr := func(v int) *int { return &v }

	testutil.RunRuleTests(t, NewNoMultipleEmptyLinesRule(), []testutil.RuleTestCase{
		// === Clean files ===
		{
			Name:           "no excess blank lines",
			Content:        "FROM alpine:3.20\n\nRUN echo hello\n\nCOPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "single blank line between instructions",
			Content:        "FROM alpine:3.20\n\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "no blank lines at all",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "minimal file",
			Content:        "FROM scratch\n",
			WantViolations: 0,
		},

		// === General excess blank lines ===
		{
			Name:           "two consecutive blank lines - one excess",
			Content:        "FROM alpine:3.20\n\n\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"too many blank lines (2), maximum allowed is 1"},
		},
		{
			Name:           "three consecutive blank lines",
			Content:        "FROM alpine:3.20\n\n\n\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"too many blank lines (3), maximum allowed is 1"},
		},
		{
			Name:           "multiple groups of excess blank lines",
			Content:        "FROM alpine:3.20\n\n\nRUN echo hello\n\n\nCOPY . /app\n",
			WantViolations: 2,
		},

		// === BOF blank lines ===
		{
			Name:           "blank line at beginning of file",
			Content:        "\nFROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"beginning of file"},
		},
		{
			Name:           "two blank lines at beginning of file",
			Content:        "\n\nFROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"beginning of file"},
		},

		// === EOF blank lines ===
		{
			Name:           "blank line at end of file",
			Content:        "FROM alpine:3.20\nRUN echo hello\n\n",
			WantViolations: 1,
			WantMessages:   []string{"end of file"},
		},
		{
			Name:           "two blank lines at end of file",
			Content:        "FROM alpine:3.20\nRUN echo hello\n\n\n",
			WantViolations: 1,
			WantMessages:   []string{"end of file"},
		},

		// === Whitespace-only blank lines ===
		{
			Name:           "whitespace-only lines count as blank",
			Content:        "FROM alpine:3.20\n  \n\t\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"too many blank lines (2)"},
		},

		// === Config: max=2 ===
		{
			Name:    "max=2 allows two blank lines",
			Content: "FROM alpine:3.20\n\n\nRUN echo hello\n",
			Config: NoMultipleEmptyLinesConfig{
				Max: intPtr(2),
			},
			WantViolations: 0,
		},
		{
			Name:    "max=2 flags three blank lines",
			Content: "FROM alpine:3.20\n\n\n\nRUN echo hello\n",
			Config: NoMultipleEmptyLinesConfig{
				Max: intPtr(2),
			},
			WantViolations: 1,
		},

		// === Config: max-bof ===
		{
			Name:    "max-bof=1 allows one blank at start",
			Content: "\nFROM alpine:3.20\nRUN echo hello\n",
			Config: NoMultipleEmptyLinesConfig{
				MaxBOF: intPtr(1),
			},
			WantViolations: 0,
		},
		{
			Name:    "max-bof=1 flags two blanks at start",
			Content: "\n\nFROM alpine:3.20\nRUN echo hello\n",
			Config: NoMultipleEmptyLinesConfig{
				MaxBOF: intPtr(1),
			},
			WantViolations: 1,
		},

		// === Config: max-eof ===
		{
			Name:    "max-eof=1 allows one blank at end",
			Content: "FROM alpine:3.20\nRUN echo hello\n\n",
			Config: NoMultipleEmptyLinesConfig{
				MaxEOF: intPtr(1),
			},
			WantViolations: 0,
		},
		{
			Name:    "max-eof=1 flags two blanks at end",
			Content: "FROM alpine:3.20\nRUN echo hello\n\n\n",
			Config: NoMultipleEmptyLinesConfig{
				MaxEOF: intPtr(1),
			},
			WantViolations: 1,
		},

		// === Heredoc handling ===
		{
			Name:           "RUN heredoc with bash - blank lines flagged",
			Content:        "FROM alpine:3.20\nRUN <<EOF\necho hello\n\n\necho world\nEOF\n",
			WantViolations: 1,
		},
		{
			Name:           "COPY heredoc - blank lines skipped",
			Content:        "FROM alpine:3.20\nCOPY <<EOF /etc/config.yml\nkey: value\n\n\nother: value\nEOF\n",
			WantViolations: 0,
		},
		{
			Name:           "RUN heredoc with python shebang - blank lines skipped",
			Content:        "FROM python:3.12\nRUN <<EOF\n#!/usr/bin/env python3\nimport sys\n\n\nprint('hello')\nEOF\n",
			WantViolations: 0,
		},
	})
}

func TestNoMultipleEmptyLinesCheckWithFixes(t *testing.T) {
	t.Parallel()
	r := NewNoMultipleEmptyLinesRule()

	tests := []struct {
		name      string
		content   string
		wantEdits int
	}{
		{
			name:      "two blank lines - one edit to delete excess",
			content:   "FROM alpine:3.20\n\n\nRUN echo hello\n",
			wantEdits: 1,
		},
		{
			name:      "multiple groups - one edit per group",
			content:   "FROM alpine:3.20\n\n\nRUN echo hello\n\n\nCOPY . /app\n",
			wantEdits: 2,
		},
		{
			name:      "blank at BOF - one edit",
			content:   "\nFROM alpine:3.20\nRUN echo hello\n",
			wantEdits: 1,
		},
		{
			name:      "blank at EOF - one edit",
			content:   "FROM alpine:3.20\nRUN echo hello\n\n",
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
