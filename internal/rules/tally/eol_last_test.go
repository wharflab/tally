package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestEolLastMetadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewEolLastRule().Metadata())
}

func TestEolLastDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := NewEolLastRule().DefaultConfig()
	got, ok := cfg.(EolLastConfig)
	if !ok {
		t.Fatalf("DefaultConfig() type = %T, want EolLastConfig", cfg)
	}
	if got.Mode == nil || *got.Mode != "always" {
		t.Errorf("Mode = %v, want always", got.Mode)
	}
}

func TestEolLastValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewEolLastRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{name: "nil config", config: nil, wantErr: false},
		{name: "empty object", config: map[string]any{}, wantErr: false},
		{name: "mode always", config: map[string]any{"mode": "always"}, wantErr: false},
		{name: "mode never", config: map[string]any{"mode": "never"}, wantErr: false},
		{name: "invalid mode", config: map[string]any{"mode": "unix"}, wantErr: true},
		{name: "extra key", config: map[string]any{"unknown": true}, wantErr: true},
		{name: "wrong type", config: map[string]any{"mode": 42}, wantErr: true},
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

func TestEolLastCheck(t *testing.T) {
	t.Parallel()

	modeNever := "never"

	testutil.RunRuleTests(t, NewEolLastRule(), []testutil.RuleTestCase{
		// === "always" mode (default) ===
		{
			Name:           "always - file ends with newline",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 0,
		},
		{
			Name:           "always - file missing final newline",
			Content:        "FROM alpine:3.20\nRUN echo hello",
			WantViolations: 1,
			WantMessages:   []string{"file must end with a newline"},
		},
		{
			Name:           "always - single instruction no newline",
			Content:        "FROM scratch",
			WantViolations: 1,
		},
		{
			Name:           "always - minimal file with newline",
			Content:        "FROM scratch\n",
			WantViolations: 0,
		},
		{
			Name:           "always - file ending with multiple newlines",
			Content:        "FROM alpine:3.20\n\n\n",
			WantViolations: 0,
		},
		{
			Name:           "always - heredoc content no final newline",
			Content:        "FROM alpine:3.20\nRUN <<EOF\necho hello\nEOF",
			WantViolations: 1,
		},

		// === "never" mode ===
		{
			Name:           "never - file without trailing newline",
			Content:        "FROM alpine:3.20\nRUN echo hello",
			Config:         EolLastConfig{Mode: &modeNever},
			WantViolations: 0,
		},
		{
			Name:           "never - file with trailing newline",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			Config:         EolLastConfig{Mode: &modeNever},
			WantViolations: 1,
			WantMessages:   []string{"file must not end with a newline"},
		},
		{
			Name:           "never - file with multiple trailing newlines",
			Content:        "FROM alpine:3.20\n\n\n",
			Config:         EolLastConfig{Mode: &modeNever},
			WantViolations: 1,
			WantMessages:   []string{"file must not end with a newline"},
		},

		// === CRLF line endings ===
		{
			Name:           "always - CRLF file ends with newline",
			Content:        "FROM alpine:3.20\r\nRUN echo hello\r\n",
			WantViolations: 0,
		},
		{
			Name:           "always - CRLF file missing final newline",
			Content:        "FROM alpine:3.20\r\nRUN echo hello",
			WantViolations: 1,
			WantMessages:   []string{"file must end with a newline"},
		},
		{
			Name:           "never - CRLF file with trailing newline",
			Content:        "FROM alpine:3.20\r\nRUN echo hello\r\n",
			Config:         EolLastConfig{Mode: &modeNever},
			WantViolations: 1,
			WantMessages:   []string{"file must not end with a newline"},
		},
		{
			Name:           "never - CRLF file without trailing newline",
			Content:        "FROM alpine:3.20\r\nRUN echo hello",
			Config:         EolLastConfig{Mode: &modeNever},
			WantViolations: 0,
		},
		{
			Name:           "never - CRLF file with multiple trailing newlines",
			Content:        "FROM alpine:3.20\r\n\r\n\r\n",
			Config:         EolLastConfig{Mode: &modeNever},
			WantViolations: 1,
			WantMessages:   []string{"file must not end with a newline"},
		},
	})
}

func TestEolLastCheckWithFixes(t *testing.T) {
	t.Parallel()
	r := NewEolLastRule()

	modeNever := "never"

	tests := []struct {
		name      string
		content   string
		config    any
		wantEdits int
		wantText  string
	}{
		{
			name:      "always - adds newline",
			content:   "FROM alpine:3.20",
			wantEdits: 1,
			wantText:  "\n",
		},
		{
			name:      "always - no fix needed",
			content:   "FROM alpine:3.20\n",
			wantEdits: 0,
		},
		{
			name:      "never - removes newline",
			content:   "FROM alpine:3.20\n",
			config:    EolLastConfig{Mode: &modeNever},
			wantEdits: 1,
			wantText:  "",
		},
		{
			name:      "never - removes multiple trailing newlines",
			content:   "FROM alpine:3.20\n\n\n",
			config:    EolLastConfig{Mode: &modeNever},
			wantEdits: 3, // one edit per trailing \n
			wantText:  "",
		},
		{
			name:      "never - no fix needed",
			content:   "FROM alpine:3.20",
			config:    EolLastConfig{Mode: &modeNever},
			wantEdits: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInputWithConfig(t, "Dockerfile", tt.content, tt.config)
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
				if tt.wantText != "" && len(v.SuggestedFix.Edits) > 0 {
					if v.SuggestedFix.Edits[0].NewText != tt.wantText {
						t.Errorf("edit NewText = %q, want %q", v.SuggestedFix.Edits[0].NewText, tt.wantText)
					}
				}
			}

			if totalEdits != tt.wantEdits {
				t.Errorf("total edits = %d, want %d", totalEdits, tt.wantEdits)
			}
		})
	}
}
