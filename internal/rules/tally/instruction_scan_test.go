package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/testutil"
)

func TestCmdFileArgCoversPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cmd        shell.CommandInfo
		targetPath string
		want       bool
	}{
		{
			name:       "exact match",
			cmd:        shell.CommandInfo{Args: []string{"-R", "user:group", "/app"}},
			targetPath: "/app",
			want:       true,
		},
		{
			name:       "child path",
			cmd:        shell.CommandInfo{Args: []string{"user:group", "/app"}},
			targetPath: "/app/file.txt",
			want:       true,
		},
		{
			name:       "deep child path",
			cmd:        shell.CommandInfo{Args: []string{"-R", "user:group", "/etc"}},
			targetPath: "/etc/app/config.conf",
			want:       true,
		},
		{
			name:       "different path",
			cmd:        shell.CommandInfo{Args: []string{"user:group", "/opt"}},
			targetPath: "/app",
			want:       false,
		},
		{
			name:       "partial name match is not coverage",
			cmd:        shell.CommandInfo{Args: []string{"user:group", "/app"}},
			targetPath: "/application",
			want:       false,
		},
		{
			name:       "multiple file args",
			cmd:        shell.CommandInfo{Args: []string{"-R", "user:group", "/opt", "/app"}},
			targetPath: "/app/bin",
			want:       true,
		},
		{
			name:       "relative path skipped",
			cmd:        shell.CommandInfo{Args: []string{"user:group", "app"}},
			targetPath: "/app",
			want:       false,
		},
		{
			name:       "empty target path",
			cmd:        shell.CommandInfo{Args: []string{"user:group", "/app"}},
			targetPath: "",
			want:       false,
		},
		{
			name:       "flags skipped",
			cmd:        shell.CommandInfo{Args: []string{"-R", "--verbose", "user:group", "/app"}},
			targetPath: "/app",
			want:       true,
		},
		{
			name:       "only owner spec no file args",
			cmd:        shell.CommandInfo{Args: []string{"user:group"}},
			targetPath: "/app",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := cmdFileArgCoversPath(&tt.cmd, tt.targetPath)
			if got != tt.want {
				t.Errorf("cmdFileArgCoversPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasFollowingRunTargetingPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		afterIdx   int // command index within first stage
		cmdName    string
		targetPath string
		want       bool
	}{
		{
			name:       "chown on same path",
			dockerfile: "FROM ubuntu\nUSER app\nCOPY app /app\nRUN chown -R app:app /app\n",
			afterIdx:   1, // after COPY (USER=0, COPY=1, RUN=2)
			cmdName:    "chown",
			targetPath: "/app",
			want:       true,
		},
		{
			name:       "chown on parent path",
			dockerfile: "FROM ubuntu\nUSER app\nCOPY config /app/config\nRUN chown -R app:app /app\n",
			afterIdx:   1,
			cmdName:    "chown",
			targetPath: "/app/config",
			want:       true,
		},
		{
			name:       "chown on different path",
			dockerfile: "FROM ubuntu\nUSER app\nCOPY app /app\nRUN chown -R app:app /opt\n",
			afterIdx:   1,
			cmdName:    "chown",
			targetPath: "/app",
			want:       false,
		},
		{
			name:       "chown not immediately next",
			dockerfile: "FROM ubuntu\nUSER app\nCOPY app /app\nEXPOSE 8080\nRUN chown -R app:app /app\n",
			afterIdx:   1,
			cmdName:    "chown",
			targetPath: "/app",
			want:       true,
		},
		{
			name:       "no following RUN",
			dockerfile: "FROM ubuntu\nUSER app\nCOPY app /app\n",
			afterIdx:   1,
			cmdName:    "chown",
			targetPath: "/app",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := testutil.MakeLintInput(t, "Dockerfile", tt.dockerfile)
			if len(input.Stages) == 0 {
				t.Fatal("no stages parsed")
			}
			got := hasFollowingRunTargetingPath(
				input.Stages[0], tt.afterIdx, tt.cmdName, tt.targetPath, shell.VariantBash,
			)
			if got != tt.want {
				t.Errorf("hasFollowingRunTargetingPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
