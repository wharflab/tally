package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNewlineBetweenInstructionsMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNewlineBetweenInstructionsRule().Metadata())
}

func TestNewlineBetweenInstructionsDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := NewNewlineBetweenInstructionsRule().DefaultConfig()
	got, ok := cfg.(NewlineBetweenInstructionsConfig)
	if !ok {
		t.Fatalf("DefaultConfig() type = %T, want NewlineBetweenInstructionsConfig", cfg)
	}
	if got.Mode != "grouped" {
		t.Errorf("Mode = %q, want %q", got.Mode, "grouped")
	}
}

func TestNewlineBetweenInstructionsValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewNewlineBetweenInstructionsRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{name: "nil config", config: nil, wantErr: false},
		{name: "string grouped", config: "grouped", wantErr: false},
		{name: "string always", config: "always", wantErr: false},
		{name: "string never", config: "never", wantErr: false},
		{name: "string invalid", config: "invalid", wantErr: true},
		{name: "object valid", config: map[string]any{"mode": "always"}, wantErr: false},
		{name: "object invalid mode", config: map[string]any{"mode": "bad"}, wantErr: true},
		{name: "object extra key", config: map[string]any{"mode": "always", "extra": true}, wantErr: true},
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

func TestNewlineBetweenInstructionsCheck(t *testing.T) {
	t.Parallel()
	testutil.RunRuleTests(t, NewNewlineBetweenInstructionsRule(), []testutil.RuleTestCase{
		// === Grouped mode (default) ===
		{
			Name:           "grouped - correct spacing pass",
			Content:        "FROM alpine:3.20\n\nRUN echo hello\n\nENV FOO=bar\nENV BAZ=qux\n\nCOPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "grouped - missing blank between different types",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"expected blank line between FROM and RUN"},
		},
		{
			Name:           "grouped - unwanted blank between same types",
			Content:        "FROM alpine:3.20\n\nRUN echo hello\nENV FOO=bar\n\nENV BAZ=qux\n",
			WantViolations: 2,
			WantMessages: []string{
				"expected blank line between RUN and ENV",
				"unexpected blank line between ENV and ENV",
			},
		},
		{
			Name:           "grouped - consecutive FROM no blank pass",
			Content:        "FROM alpine:3.20 AS builder\nFROM scratch\n",
			WantViolations: 0,
		},
		{
			Name:           "grouped - consecutive FROM with unwanted blank",
			Content:        "FROM alpine:3.20 AS builder\n\nFROM scratch\n",
			WantViolations: 1,
			WantMessages:   []string{"unexpected blank line between FROM and FROM"},
		},
		{
			Name:           "grouped - excess blanks between different types",
			Content:        "FROM alpine:3.20\n\n\n\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"expected 1 blank line between FROM and RUN, found 3"},
		},

		// === Always mode ===
		{
			Name:           "always - blanks everywhere pass",
			Content:        "FROM alpine:3.20\n\nRUN echo hello\n\nENV FOO=bar\n\nENV BAZ=qux\n",
			Config:         "always",
			WantViolations: 0,
		},
		{
			Name:           "always - missing blanks",
			Content:        "FROM alpine:3.20\nRUN echo hello\nENV FOO=bar\n",
			Config:         "always",
			WantViolations: 2,
			WantMessages: []string{
				"expected blank line between FROM and RUN",
				"expected blank line between RUN and ENV",
			},
		},
		{
			Name:           "always - multiple blanks acceptable",
			Content:        "FROM alpine:3.20\n\n\nRUN echo hello\n",
			Config:         "always",
			WantViolations: 0,
		},

		// === Never mode ===
		{
			Name:           "never - no blanks pass",
			Content:        "FROM alpine:3.20\nRUN echo hello\nENV FOO=bar\n",
			Config:         "never",
			WantViolations: 0,
		},
		{
			Name:           "never - blanks present",
			Content:        "FROM alpine:3.20\n\nRUN echo hello\n",
			Config:         "never",
			WantViolations: 1,
			WantMessages:   []string{"unexpected blank line between FROM and RUN"},
		},
		{
			Name:           "never - multiple blanks removed",
			Content:        "FROM alpine:3.20\n\n\n\nRUN echo hello\n",
			Config:         "never",
			WantViolations: 1,
			WantMessages:   []string{"unexpected blank line between FROM and RUN"},
		},

		// === Edge cases ===
		{
			Name:           "single instruction",
			Content:        "FROM alpine:3.20\n",
			WantViolations: 0,
		},
		{
			Name:           "global ARG before FROM - grouped",
			Content:        "ARG BASE=alpine\n\nFROM ${BASE}\n",
			WantViolations: 0,
		},
		{
			Name:           "global ARG before FROM - grouped missing blank",
			Content:        "ARG BASE=alpine\nFROM ${BASE}\n",
			WantViolations: 1,
			WantMessages:   []string{"expected blank line between ARG and FROM"},
		},
		{
			Name:           "comments between instructions",
			Content:        "FROM alpine:3.20\n\n# Install dependencies\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "comments between instructions - missing blank before comment",
			Content:        "FROM alpine:3.20\n# Install dependencies\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"expected blank line between FROM and RUN"},
		},
		{
			Name:           "grouped - same type with comment and blank lines",
			Content:        "FROM alpine:3.20\n\nRUN echo foo\n\n# some comment\n\nRUN echo zoo\n",
			WantViolations: 1,
			WantMessages:   []string{"unexpected blank line between RUN and RUN"},
		},
		{
			Name:           "grouped - different types with comment and blank lines",
			Content:        "FROM alpine:3.20\n\n# some comment\n\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"expected 1 blank line between FROM and RUN, found 2"},
		},
		{
			Name:           "grouped - same type with attached comment pass",
			Content:        "FROM alpine:3.20\n\nRUN echo foo\n# some comment\nRUN echo zoo\n",
			WantViolations: 0,
		},
		{
			Name:           "parser directive - correct spacing pass",
			Content:        "# syntax=docker/dockerfile:1\nFROM alpine:3.20\n\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "parser directives - missing blank",
			Content:        "# syntax=docker/dockerfile:1\n# escape=`\nFROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"expected blank line between FROM and RUN"},
		},
		{
			Name:           "inline disable with correct spacing pass",
			Content:        "FROM alpine:3.20\n\n# tally ignore=tally/some-rule\nRUN echo hello\n\nCOPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "inline disable - missing blank before comment",
			Content:        "FROM alpine:3.20\n# tally ignore=tally/some-rule\nRUN echo hello\n",
			WantViolations: 1,
			WantMessages:   []string{"expected blank line between FROM and RUN"},
		},
		{
			Name:           "multi-line continuation",
			Content:        "FROM alpine:3.20\n\nRUN echo hello \\\n    && echo world\n\nCOPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "heredoc instruction",
			Content:        "FROM alpine:3.20\n\nRUN <<EOF\necho hello\nEOF\n\nCOPY . /app\n",
			WantViolations: 0,
		},
		{
			Name:           "string shorthand config",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			Config:         "never",
			WantViolations: 0,
		},
		{
			Name:           "object config",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			Config:         map[string]any{"mode": "never"},
			WantViolations: 0,
		},
	})
}

func TestNewlineBetweenInstructionsCheckWithFixes(t *testing.T) {
	t.Parallel()
	r := NewNewlineBetweenInstructionsRule()

	tests := []struct {
		name           string
		content        string
		config         any
		wantViolations int
		wantMode       string
	}{
		{
			name:           "insert blank line",
			content:        "FROM alpine:3.20\nRUN echo hello\n",
			wantViolations: 1,
			wantMode:       "grouped",
		},
		{
			name:           "remove blank line",
			content:        "FROM alpine:3.20\n\nRUN echo hello\nENV FOO=bar\n\nENV BAZ=qux\n",
			wantViolations: 2,
			wantMode:       "grouped",
		},
		{
			name:           "grouped - excess blanks reduced to one",
			content:        "FROM alpine:3.20\n\n\n\nRUN echo hello\n",
			wantViolations: 1,
			wantMode:       "grouped",
		},
		{
			name:           "remove multiple blank lines - never mode",
			content:        "FROM alpine:3.20\n\n\n\nRUN echo hello\n",
			config:         "never",
			wantViolations: 1,
			wantMode:       "never",
		},
		{
			name:           "always mode insert",
			content:        "FROM alpine:3.20\nRUN echo hello\nENV FOO=bar\n",
			config:         "always",
			wantViolations: 2,
			wantMode:       "always",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, tt.config)
			violations := r.Check(input)

			if len(violations) != tt.wantViolations {
				t.Fatalf("violations = %d, want %d", len(violations), tt.wantViolations)
			}

			for _, v := range violations {
				if v.SuggestedFix == nil {
					t.Error("violation has no SuggestedFix")
					continue
				}
				if v.SuggestedFix.Safety != rules.FixSafe {
					t.Errorf("fix safety = %v, want FixSafe", v.SuggestedFix.Safety)
				}
				if !v.SuggestedFix.NeedsResolve {
					t.Error("expected NeedsResolve=true")
				}
				if v.SuggestedFix.ResolverID != rules.NewlineResolverID {
					t.Errorf("ResolverID = %q, want %q", v.SuggestedFix.ResolverID, rules.NewlineResolverID)
				}
				data, ok := v.SuggestedFix.ResolverData.(*rules.NewlineResolveData)
				if !ok || data == nil {
					t.Error("expected *rules.NewlineResolveData")
				} else if data.Mode != tt.wantMode {
					t.Errorf("ResolverData.Mode = %q, want %q", data.Mode, tt.wantMode)
				}
			}
		})
	}
}
