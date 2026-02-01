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
				TargetDir:         "/app",
				IsStandalone:      false,
				IsAtStart:         false,
				PrecedingCommands: "make build",
			},
		},
		{
			name:      "cd in middle of chain",
			script:    "mkdir /tmp && cd /tmp && make build",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:         "/tmp",
				IsStandalone:      false,
				IsAtStart:         false,
				PrecedingCommands: "mkdir /tmp",
				RemainingCommands: "make build",
			},
		},
		{
			name:      "cd in middle of longer chain",
			script:    "git clone repo && cd repo && make && make install",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:         "repo",
				IsStandalone:      false,
				IsAtStart:         false,
				PrecedingCommands: "git clone repo",
				RemainingCommands: "make && make install",
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
				TargetDir:         "/app",
				IsStandalone:      false,
				IsAtStart:         true,
				RemainingCommands: "make && make install", // Full chain after cd
			},
		},
		{
			name:      "semicolon-separated commands",
			script:    "cd /app; make",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:    "/app",
				IsStandalone: false, // Not standalone - make follows
				IsAtStart:    true,
			},
		},
		{
			name:      "cd after semicolon",
			script:    "echo hello; cd /app",
			wantCount: 1,
			wantFirst: &CdCommand{
				TargetDir:    "/app",
				IsStandalone: false, // Not standalone - echo precedes
				IsAtStart:    false, // Not at start - it's the second statement
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
				assert.Equal(t, tt.wantFirst.PrecedingCommands, got.PrecedingCommands, "PrecedingCommands")
				assert.Equal(t, tt.wantFirst.RemainingCommands, got.RemainingCommands, "RemainingCommands")
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
		{"cd /app; make", false}, // semicolon-separated is not standalone
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
