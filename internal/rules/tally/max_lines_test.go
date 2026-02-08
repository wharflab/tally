package tally

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

// Helper functions for pointer values in tests.
func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }
func boolTrue() *bool      { return boolPtr(true) }
func boolFalse() *bool     { return boolPtr(false) }

func TestMaxLinesRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewMaxLinesRule().Metadata())
}

func TestMaxLinesRule_Check(t *testing.T) {
	t.Parallel()
	testutil.RunRuleTests(t, NewMaxLinesRule(), []testutil.RuleTestCase{
		{
			Name:           "disabled when max is 0",
			Content:        "FROM alpine\nRUN echo hello\nRUN echo world",
			Config:         MaxLinesConfig{Max: intPtr(0)},
			WantViolations: 0,
		},
		{
			Name:           "no violation when under limit",
			Content:        "FROM alpine",
			Config:         MaxLinesConfig{Max: intPtr(10)},
			WantViolations: 0,
		},
		{
			Name:           "no violation when at limit",
			Content:        "FROM alpine\nRUN echo hello",
			Config:         MaxLinesConfig{Max: intPtr(2)},
			WantViolations: 0,
		},
		{
			Name:           "violation when over limit",
			Content:        "FROM alpine\nRUN echo hello\nRUN echo world",
			Config:         MaxLinesConfig{Max: intPtr(2)},
			WantViolations: 1,
			WantCodes:      []string{"tally/max-lines"},
			WantMessages:   []string{"file has 3 lines, maximum allowed is 2"},
		},
		{
			Name:           "skip blank lines",
			Content:        "FROM alpine\n\nRUN echo hello\n\n",
			Config:         MaxLinesConfig{Max: intPtr(2), SkipBlankLines: boolTrue()},
			WantViolations: 0, // Only 2 non-blank lines
		},
		{
			Name:           "count blank lines when false",
			Content:        "FROM alpine\n\nRUN echo hello",
			Config:         MaxLinesConfig{Max: intPtr(2), SkipBlankLines: boolFalse()},
			WantViolations: 1, // 3 lines including blank
		},
		{
			Name:           "skip comments",
			Content:        "# This is a comment\nFROM alpine\n# Another comment",
			Config:         MaxLinesConfig{Max: intPtr(1), SkipComments: boolTrue()},
			WantViolations: 0, // Only 1 non-comment line
		},
		{
			Name:           "count comments when false",
			Content:        "# Comment\nFROM alpine",
			Config:         MaxLinesConfig{Max: intPtr(1), SkipComments: boolFalse()},
			WantViolations: 1, // 2 lines including comment
		},
		{
			Name:           "skip both blank and comments",
			Content:        "# Comment\nFROM alpine\n\nRUN echo hello\n# Another",
			Config:         MaxLinesConfig{Max: intPtr(2), SkipBlankLines: boolTrue(), SkipComments: boolTrue()},
			WantViolations: 0, // Only 2 code lines
		},
		{
			Name:           "nil config uses defaults",
			Content:        "FROM alpine\nRUN echo hello\nRUN echo world",
			Config:         nil,
			WantViolations: 0, // Default max is 50, content is only 3 lines
		},
		// Trailing newline behavior:
		// When SkipBlankLines is false, ALL blank lines count - including trailing ones.
		// A single trailing \n is just a line terminator, not a blank line.
		{
			Name:           "single trailing newline is just terminator",
			Content:        "FROM alpine\nRUN echo hello\n",
			Config:         MaxLinesConfig{Max: intPtr(2), SkipBlankLines: boolFalse()},
			WantViolations: 0, // 2 lines - trailing \n is line terminator
		},
		{
			Name:           "trailing blank lines count when not skipped",
			Content:        "FROM alpine\nRUN echo hello\n\n\n",
			Config:         MaxLinesConfig{Max: intPtr(2), SkipBlankLines: boolFalse()},
			WantViolations: 1, // 4 lines (2 content + 2 trailing blanks)
			WantMessages:   []string{"file has 4 lines"},
		},
		{
			Name:           "trailing blanks ignored when skipping blanks",
			Content:        "FROM alpine\nRUN echo hello\n\n\n",
			Config:         MaxLinesConfig{Max: intPtr(2), SkipBlankLines: boolTrue()},
			WantViolations: 0, // 2 lines - all blanks skipped
		},
		{
			Name:           "blank lines between instructions count",
			Content:        "FROM alpine\n\n\nRUN echo hello",
			Config:         MaxLinesConfig{Max: intPtr(2), SkipBlankLines: boolFalse()},
			WantViolations: 1, // 4 lines - blanks within content span count
			WantMessages:   []string{"file has 4 lines"},
		},
	})
}

func TestMaxLinesRule_Interfaces(t *testing.T) {
	t.Parallel()
	r := NewMaxLinesRule()

	// Verify Rule interface
	var _ rules.Rule = r

	// Verify ConfigurableRule interface
	var _ rules.ConfigurableRule = r
}

func TestMaxLinesRule_DefaultConfig(t *testing.T) {
	t.Parallel()
	r := NewMaxLinesRule()
	cfg := r.DefaultConfig()

	defCfg, ok := cfg.(MaxLinesConfig)
	if !ok {
		t.Fatalf("DefaultConfig() returned %T, want MaxLinesConfig", cfg)
	}
	// Default: 50 (P90 of 500 analyzed Dockerfiles)
	if defCfg.Max == nil || *defCfg.Max != 50 {
		t.Errorf("default Max = %v, want 50", defCfg.Max)
	}
	// Default: true (count only meaningful lines)
	if defCfg.SkipBlankLines == nil || !*defCfg.SkipBlankLines {
		t.Error("default SkipBlankLines should be true")
	}
	// Default: true (count only instruction lines)
	if defCfg.SkipComments == nil || !*defCfg.SkipComments {
		t.Error("default SkipComments should be true")
	}
}

func TestMaxLinesRule_ValidateConfig(t *testing.T) {
	t.Parallel()
	r := NewMaxLinesRule()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  MaxLinesConfig{Max: intPtr(100)},
			wantErr: false,
		},
		{
			name:    "zero max is valid (disables rule)",
			config:  MaxLinesConfig{Max: intPtr(0)},
			wantErr: false,
		},
		{
			name:    "negative max is invalid",
			config:  MaxLinesConfig{Max: intPtr(-1)},
			wantErr: true,
		},
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "pointer config is valid",
			config:  &MaxLinesConfig{Max: intPtr(50)},
			wantErr: false,
		},
		{
			name:    "nil pointer config is valid",
			config:  (*MaxLinesConfig)(nil),
			wantErr: false,
		},
		{
			name:    "pointer with negative max is invalid",
			config:  &MaxLinesConfig{Max: intPtr(-5)},
			wantErr: true,
		},
		{
			name:    "nil Max field is valid (uses default)",
			config:  MaxLinesConfig{Max: nil},
			wantErr: false,
		},
		{
			name:    "wrong type",
			config:  "not a config",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := r.ValidateConfig(tc.config)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
