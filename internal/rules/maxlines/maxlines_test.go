package maxlines

import (
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/testutil"
)

func TestRule_Metadata(t *testing.T) {
	r := New()
	meta := r.Metadata()

	if meta.Code != "max-lines" {
		t.Errorf("Code = %q, want %q", meta.Code, "max-lines")
	}
	// Enabled by default with sensible defaults (50 lines, skip blanks/comments)
	if !meta.EnabledByDefault {
		t.Error("EnabledByDefault should be true")
	}
}

func TestRule_Check(t *testing.T) {
	testutil.RunRuleTests(t, New(), []testutil.RuleTestCase{
		{
			Name:           "disabled when max is 0",
			Content:        "FROM alpine\nRUN echo hello\nRUN echo world",
			Config:         Config{Max: 0},
			WantViolations: 0,
		},
		{
			Name:           "no violation when under limit",
			Content:        "FROM alpine",
			Config:         Config{Max: 10},
			WantViolations: 0,
		},
		{
			Name:           "no violation when at limit",
			Content:        "FROM alpine\nRUN echo hello",
			Config:         Config{Max: 2},
			WantViolations: 0,
		},
		{
			Name:           "violation when over limit",
			Content:        "FROM alpine\nRUN echo hello\nRUN echo world",
			Config:         Config{Max: 2},
			WantViolations: 1,
			WantCodes:      []string{"max-lines"},
			WantMessages:   []string{"file has 3 lines, maximum allowed is 2"},
		},
		{
			Name:           "skip blank lines",
			Content:        "FROM alpine\n\nRUN echo hello\n\n",
			Config:         Config{Max: 2, SkipBlankLines: true},
			WantViolations: 0, // Only 2 non-blank lines
		},
		{
			Name:           "count blank lines by default",
			Content:        "FROM alpine\n\nRUN echo hello",
			Config:         Config{Max: 2},
			WantViolations: 1, // 3 lines including blank
		},
		{
			Name:           "skip comments",
			Content:        "# This is a comment\nFROM alpine\n# Another comment",
			Config:         Config{Max: 1, SkipComments: true},
			WantViolations: 0, // Only 1 non-comment line
		},
		{
			Name:           "count comments by default",
			Content:        "# Comment\nFROM alpine",
			Config:         Config{Max: 1},
			WantViolations: 1, // 2 lines including comment
		},
		{
			Name:           "skip both blank and comments",
			Content:        "# Comment\nFROM alpine\n\nRUN echo hello\n# Another",
			Config:         Config{Max: 2, SkipBlankLines: true, SkipComments: true},
			WantViolations: 0, // Only 2 code lines
		},
		{
			Name:           "nil config uses defaults (disabled)",
			Content:        "FROM alpine\nRUN echo hello\nRUN echo world",
			Config:         nil,
			WantViolations: 0, // Default max is 0 (disabled)
		},
	})
}

func TestRule_Interfaces(t *testing.T) {
	r := New()

	// Verify Rule interface
	var _ rules.Rule = r

	// Verify ConfigurableRule interface
	var _ rules.ConfigurableRule = r
}

func TestRule_DefaultConfig(t *testing.T) {
	r := New()
	cfg := r.DefaultConfig()

	defCfg, ok := cfg.(Config)
	if !ok {
		t.Fatalf("DefaultConfig() returned %T, want Config", cfg)
	}
	// Default: 50 (P90 of 500 analyzed Dockerfiles)
	if defCfg.Max != 50 {
		t.Errorf("default Max = %d, want 50", defCfg.Max)
	}
	// Default: true (count only meaningful lines)
	if !defCfg.SkipBlankLines {
		t.Error("default SkipBlankLines should be true")
	}
	// Default: true (count only instruction lines)
	if !defCfg.SkipComments {
		t.Error("default SkipComments should be true")
	}
}

func TestRule_ValidateConfig(t *testing.T) {
	r := New()

	tests := []struct {
		name    string
		config  any
		wantErr bool
	}{
		{
			name:    "valid config",
			config:  Config{Max: 100},
			wantErr: false,
		},
		{
			name:    "zero max is valid",
			config:  Config{Max: 0},
			wantErr: false,
		},
		{
			name:    "negative max is invalid",
			config:  Config{Max: -1},
			wantErr: true,
		},
		{
			name:    "nil config is valid",
			config:  nil,
			wantErr: false,
		},
		{
			name:    "pointer config is valid",
			config:  &Config{Max: 50},
			wantErr: false,
		},
		{
			name:    "nil pointer config is valid",
			config:  (*Config)(nil),
			wantErr: false,
		},
		{
			name:    "pointer with negative max is invalid",
			config:  &Config{Max: -5},
			wantErr: true,
		},
		{
			name:    "wrong type",
			config:  "not a config",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := r.ValidateConfig(tc.config)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
