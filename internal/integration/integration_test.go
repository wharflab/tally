package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

var (
	binaryPath  string
	coverageDir string
)

func TestMain(m *testing.M) {
	// Build the binary once before running tests
	tmpDir, err := os.MkdirTemp("", "tally-test")
	if err != nil {
		panic(err)
	}

	binaryName := "tally"
	if runtime.GOOS == "windows" {
		binaryName = "tally.exe"
	}
	binaryPath = filepath.Join(tmpDir, binaryName)

	// Create coverage directory in project root for persistent coverage data
	// If GOCOVERDIR is set externally, use that; otherwise use "./coverage"
	coverageDir = os.Getenv("GOCOVERDIR")
	if coverageDir == "" {
		// Get absolute path to project root (2 levels up from internal/integration)
		wd, err := os.Getwd()
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			panic("failed to get working directory: " + err.Error())
		}
		coverageDir = filepath.Join(wd, "..", "..", "coverage")
	}
	// Make path absolute
	coverageDir, err = filepath.Abs(coverageDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		panic("failed to get absolute coverage directory path: " + err.Error())
	}
	if err := os.MkdirAll(coverageDir, 0o750); err != nil {
		_ = os.RemoveAll(tmpDir)
		panic("failed to create coverage directory: " + err.Error())
	}

	// Build the module's main package with coverage instrumentation
	cmd := exec.Command("go", "build", "-cover", "-o", binaryPath, "github.com/tinovyatkin/tally")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		panic("failed to build binary: " + string(out))
	}

	code := m.Run()

	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

func TestCheck(t *testing.T) {
	testCases := []struct {
		name       string
		dir        string
		args       []string
		env        []string
		wantExit   int
		snapExt    string // Snapshot file extension (default: ".json")
		isDir      bool   // If true, pass the directory instead of Dockerfile
		useContext bool   // If true, add --context flag for context-aware tests
	}{
		// Basic tests
		{name: "simple", dir: "simple", args: []string{"--format", "json"}},
		{
			name: "simple-max-lines-pass",
			dir:  "simple",
			args: []string{"--max-lines", "100", "--format", "json"},
		},
		{
			name:     "simple-max-lines-fail",
			dir:      "simple",
			args:     []string{"--max-lines", "2", "--format", "json"},
			wantExit: 1,
		},

		// Config file discovery tests
		{name: "config-file-discovery", dir: "with-config", args: []string{"--format", "json"}, wantExit: 1},
		{name: "config-cascading-discovery", dir: "nested/subdir", args: []string{"--format", "json"}, wantExit: 1},
		{name: "config-skip-options", dir: "with-blanks-and-comments", args: []string{"--format", "json"}},
		{
			name: "cli-overrides-config",
			dir:  "with-config",
			args: []string{"--max-lines", "100", "--format", "json"},
		},

		// Environment variable tests
		{
			name:     "env-var-override",
			dir:      "simple",
			args:     []string{"--format", "json"},
			env:      []string{"TALLY_RULES_MAX_LINES_MAX=2"},
			wantExit: 1,
		},

		// BuildKit linter warnings tests
		{name: "buildkit-warnings", dir: "buildkit-warnings", args: []string{"--format", "json"}, wantExit: 1},

		// Semantic model construction-time violations
		{name: "duplicate-stage-name", dir: "duplicate-stage-name", args: []string{"--format", "json"}, wantExit: 1},

		// Unreachable stage detection
		{name: "unreachable-stage", dir: "unreachable-stage", args: []string{"--format", "json"}, wantExit: 1},

		// Inline directive tests
		{name: "inline-ignore-single", dir: "inline-ignore-single", args: []string{"--format", "json"}},
		{name: "inline-ignore-global", dir: "inline-ignore-global", args: []string{"--format", "json"}},
		{name: "inline-hadolint-compat", dir: "inline-hadolint-compat", args: []string{"--format", "json"}},
		{name: "inline-buildx-compat", dir: "inline-buildx-compat", args: []string{"--format", "json"}},
		{name: "inline-ignore-multiple-max-lines", dir: "inline-ignore-multiple", args: []string{"--format", "json"}},
		{
			name:     "inline-unused-directive",
			dir:      "inline-unused-directive",
			args:     []string{"--format", "json", "--warn-unused-directives"},
			wantExit: 1,
		},
		{
			name:     "inline-directives-disabled",
			dir:      "inline-directives-disabled",
			args:     []string{"--format", "json", "--no-inline-directives"},
			wantExit: 1,
		},
		{
			name:     "inline-require-reason",
			dir:      "inline-require-reason",
			args:     []string{"--format", "json", "--require-reason"},
			wantExit: 1,
		},

		// Output format tests
		{name: "format-sarif", dir: "buildkit-warnings", args: []string{"--format", "sarif"}, wantExit: 1},
		{
			name:     "format-github-actions",
			dir:      "buildkit-warnings",
			args:     []string{"--format", "github-actions"},
			wantExit: 1,
			snapExt:  ".txt",
		},
		{
			name:     "format-markdown",
			dir:      "buildkit-warnings",
			args:     []string{"--format", "markdown"},
			wantExit: 1,
			snapExt:  ".md",
		},

		// Fail-level tests
		{
			name: "fail-level-none",
			dir:  "buildkit-warnings",
			args: []string{"--format", "json", "--fail-level", "none"},
		},
		{
			name: "fail-level-error",
			dir:  "buildkit-warnings",
			args: []string{"--format", "json", "--fail-level", "error"},
		},
		{
			name:     "fail-level-warning",
			dir:      "buildkit-warnings",
			args:     []string{"--format", "json", "--fail-level", "warning"},
			wantExit: 1,
		},

		// Context-aware rule tests
		{
			name:       "context-copy-ignored",
			dir:        "context-copy-ignored",
			args:       []string{"--format", "json"},
			wantExit:   1,
			useContext: true,
		},
		{
			name:       "context-copy-heredoc",
			dir:        "context-copy-heredoc",
			args:       []string{"--format", "json"},
			useContext: true,
		},
		{
			name: "context-no-context-flag",
			dir:  "context-copy-ignored",
			args: []string{"--format", "json"},
		},

		// Discovery tests
		{
			name:  "discovery-directory",
			dir:   "discovery-directory",
			args:  []string{"--format", "json"},
			isDir: true,
		},
		{
			name:  "discovery-exclude",
			dir:   "discovery-exclude",
			args:  []string{"--format", "json", "--exclude", "test/*", "--exclude", "vendor/*"},
			isDir: true,
		},
		{
			name:     "per-file-configs",
			dir:      "per-file-configs",
			args:     []string{"--format", "json"},
			isDir:    true,
			wantExit: 1,
		},

		// Rule-specific tests
		{
			name: "trusted-registries-allowed",
			dir:  "trusted-registries-allowed",
			args: []string{"--format", "json"},
		},
		{
			name:     "trusted-registries-untrusted",
			dir:      "trusted-registries-untrusted",
			args:     []string{"--format", "json"},
			wantExit: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testdataDir := filepath.Join("testdata", tc.dir)

			args := make([]string, 0, 1+len(tc.args)+2)
			args = append(args, "check")
			args = append(args, tc.args...)

			// Add context flag for context-aware tests
			if tc.useContext {
				args = append(args, "--context", testdataDir)
			}

			// Add target (directory or file)
			if tc.isDir {
				args = append(args, testdataDir)
			} else {
				args = append(args, filepath.Join(testdataDir, "Dockerfile"))
			}

			cmd := exec.Command(binaryPath, args...)
			cmd.Env = append(os.Environ(),
				"GOCOVERDIR="+coverageDir,
			)
			// Add test-specific environment variables
			cmd.Env = append(cmd.Env, tc.env...)
			output, err := cmd.CombinedOutput()

			// Check exit code
			exitCode := 0
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					exitCode = exitErr.ExitCode()
				} else {
					t.Fatalf("command failed to start: %v", err)
				}
			}
			if exitCode != tc.wantExit {
				t.Errorf("expected exit code %d, got %d\noutput: %s", tc.wantExit, exitCode, output)
			}

			// Use appropriate snapshot extension based on output format
			ext := tc.snapExt
			if ext == "" {
				ext = ".json"
			}

			// Normalize output for cross-platform snapshot comparison
			outputStr := string(output)
			// Normalize line endings (Windows CRLF -> LF) for consistent snapshots
			outputStr = strings.ReplaceAll(outputStr, "\r\n", "\n")

			if tc.isDir {
				// Replace absolute paths with relative ones for reproducible snapshots
				wd, err := os.Getwd()
				if err == nil {
					wdSlash := filepath.ToSlash(wd) + "/"
					outputStr = strings.ReplaceAll(outputStr, wdSlash, "")
				}
			}

			snaps.WithConfig(snaps.Ext(ext)).MatchStandaloneSnapshot(t, outputStr)
		})
	}
}

func TestVersion(t *testing.T) {
	cmd := exec.Command(binaryPath, "version")
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("version command failed: %v\noutput: %s", err, output)
	}

	// Version output contains "dev" in tests
	if len(output) == 0 {
		t.Error("expected version output, got empty")
	}
}
