package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFindCdCommands(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		wantCount  int
		wantFirst  *CdCommand // Expected first result (nil to skip check)
	}{
		{
			name:      "standalone cd",
			script:    "cd /opt",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:    "/opt",
				IsStandalone: true,
				IsAtStart:    true,
			},
		},
		{
			name:      "cd at start with chain",
			script:    "cd /app && make build",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:         "/app",
				IsStandalone:      false,
				IsAtStart:         true,
				RemainingCommands: "make build",
			},
		},
		{
			name:      "cd at end of chain",
			script:    "make build && cd /app",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:    "/app",
				IsStandalone: false,
				IsAtStart:    false,
			},
		},
		{
			name:      "no cd command",
			script:    "make build && echo done",
			wantCount: 0,
		},
		{
			name:      "cd with quoted path",
			script:    `cd "/path with spaces"`,
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:    "/path with spaces",
				IsStandalone: true,
				IsAtStart:    true,
			},
		},
		{
			name:      "cd with variable",
			script:    "cd $HOME",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:    "", // Variables don't resolve to literals
				IsStandalone: true,
				IsAtStart:    true,
			},
		},
		{
			name:      "multiple commands after cd",
			script:    "cd /app && make && make install",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:    "/app",
				IsStandalone: false,
				IsAtStart:    true,
				// Note: RemainingCommands extracts the immediate right-hand statement,
				// which may be just the first command in a longer chain.
				// This is sufficient for our fix detection purposes.
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := FindCdCommands(tt.script, VariantBash)
			assert.Len(t, results, tt.wantCount)

			if tt.wantFirst != nil && len(results) > 0 {
				got := results[0]
				assert.Equal(t, tt.wantFirst.TargetDir, got.TargetDir, "TargetDir")
				assert.Equal(t, tt.wantFirst.IsStandalone, got.IsStandalone, "IsStandalone")
				assert.Equal(t, tt.wantFirst.IsAtStart, got.IsAtStart, "IsAtStart")
				if tt.wantFirst.RemainingCommands != "" {
					assert.Equal(t, tt.wantFirst.RemainingCommands, got.RemainingCommands, "RemainingCommands")
				}
			}
		})
	}
}

func TestHasStandaloneCd(t *testing.T) {
	tests := []struct {
		script string
		want   bool
	}{
		{"cd /opt", true},
		{"cd /app && make", false},
		{"make && cd /app", false},
		{"echo hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.script, func(t *testing.T) {
			got := HasStandaloneCd(tt.script, VariantBash)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasCdAtStart(t *testing.T) {
	tests := []struct {
		script string
		want   bool
	}{
		{"cd /opt", true},
		{"cd /app && make", true},
		{"make && cd /app", false},
		{"echo hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.script, func(t *testing.T) {
			got := HasCdAtStart(tt.script, VariantBash)
			assert.Equal(t, tt.want, got)
		})
	}
}
