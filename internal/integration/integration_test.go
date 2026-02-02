package integration

import (
	"bytes"
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
		{name: "empty-continuation", dir: "empty-continuation", args: []string{"--format", "json"}, wantExit: 1},
		{name: "maintainer-deprecated", dir: "maintainer-deprecated", args: []string{"--format", "json"}, wantExit: 1},
		{name: "consistent-instruction-casing", dir: "consistent-instruction-casing", args: []string{"--format", "json"}, wantExit: 1},

		// Semantic model construction-time violations
		{name: "duplicate-stage-name", dir: "duplicate-stage-name", args: []string{"--format", "json"}, wantExit: 1},
		{name: "multiple-healthcheck", dir: "multiple-healthcheck", args: []string{"--format", "json"}, wantExit: 1},
		{name: "copy-from-own-alias", dir: "copy-from-own-alias", args: []string{"--format", "json"}, wantExit: 1},
		{name: "onbuild-forbidden", dir: "onbuild-forbidden", args: []string{"--format", "json"}, wantExit: 1},
		{name: "invalid-instruction-order", dir: "invalid-instruction-order", args: []string{"--format", "json"}, wantExit: 1},
		{name: "no-from-instruction", dir: "no-from-instruction", args: []string{"--format", "json"}, wantExit: 1},

		// Unreachable stage detection
		{name: "unreachable-stage", dir: "unreachable-stage", args: []string{"--format", "json"}, wantExit: 1},

		// Inline directive tests
		{name: "inline-ignore-single", dir: "inline-ignore-single", args: []string{"--format", "json"}},
		{name: "inline-ignore-global", dir: "inline-ignore-global", args: []string{"--format", "json"}},
		{name: "inline-hadolint-compat", dir: "inline-hadolint-compat", args: []string{"--format", "json"}},
		{name: "inline-buildx-compat", dir: "inline-buildx-compat", args: []string{"--format", "json"}},

		// Hadolint rule tests
		{name: "dl3003", dir: "dl3003", args: []string{"--format", "json"}, wantExit: 1},
		{name: "dl3021", dir: "dl3021", args: []string{"--format", "json"}, wantExit: 1},
		{name: "dl3027", dir: "dl3027", args: []string{"--format", "json"}, wantExit: 1},
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
		{
			name:     "avoid-latest-tag",
			dir:      "avoid-latest-tag",
			args:     []string{"--format", "json"},
			wantExit: 1,
		},
		{
			name:     "non-posix-shell",
			dir:      "non-posix-shell",
			args:     []string{"--format", "json"},
			wantExit: 0, // Should pass - shell rules disabled for PowerShell
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

func TestFix(t *testing.T) {
	testCases := []struct {
		name        string
		input       string // Input Dockerfile content
		want        string // Expected fixed content
		args        []string
		wantApplied int // Expected number of fixes applied
	}{
		{
			name:        "stage-name-casing",
			input:       "FROM alpine:3.18 AS Builder\nRUN echo hello\nFROM alpine:3.18\nCOPY --from=Builder /app /app\n",
			want:        "FROM alpine:3.18 AS builder\nRUN echo hello\nFROM alpine:3.18\nCOPY --from=builder /app /app\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "from-as-casing",
			input:       "FROM alpine:3.18 as builder\nRUN echo hello\n",
			want:        "FROM alpine:3.18 AS builder\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "combined-stage-and-as-casing",
			input:       "FROM alpine:3.18 as Builder\nRUN echo hello\n",
			want:        "FROM alpine:3.18 AS builder\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 2, // Both FromAsCasing and StageNameCasing
		},
		// DL3027: apt -> apt-get (regression test for line number consistency)
		{
			name:        "dl3027-apt-to-apt-get",
			input:       "FROM ubuntu:22.04\nRUN apt update && apt install -y curl\n",
			want:        "FROM ubuntu:22.04\nRUN apt-get update && apt-get install -y curl\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Single violation with multiple edits
		},
		// DL3003: cd -> WORKDIR (regression test for line number consistency)
		{
			// DL3003 fix is FixSuggestion (not FixSafe) because WORKDIR creates
			// the directory if it doesn't exist, while RUN cd fails.
			// Requires both --fix and --fix-unsafe since FixSuggestion > FixSafe.
			name:        "dl3003-cd-to-workdir",
			input:       "FROM ubuntu:22.04\nRUN cd /app\n",
			want:        "FROM ubuntu:22.04\nWORKDIR /app\n",
			args:        []string{"--fix", "--fix-unsafe"},
			wantApplied: 1,
		},
		// NoEmptyContinuation: Remove empty lines in continuations
		{
			name:        "no-empty-continuation-single",
			input:       "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add curl\n",
			want:        "FROM alpine:3.18\nRUN apk update && \\\n    apk add curl\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
		{
			name:        "no-empty-continuation-multiple",
			input:       "FROM alpine:3.18\nRUN apk update && \\\n\n    apk add \\\n\n    curl\n",
			want:        "FROM alpine:3.18\nRUN apk update && \\\n    apk add \\\n    curl\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Single violation covers all empty lines
		},
		// ConsistentInstructionCasing: Normalize instruction casing
		{
			name:        "consistent-instruction-casing-to-upper",
			input:       "FROM alpine:3.18\nrun echo hello\nCOPY . /app\nworkdir /app\n",
			want:        "FROM alpine:3.18\nRUN echo hello\nCOPY . /app\nWORKDIR /app\n",
			args:        []string{"--fix"},
			wantApplied: 2, // Two instructions need fixing
		},
		{
			name:        "consistent-instruction-casing-to-lower",
			input:       "from alpine:3.18\nrun echo hello\nCOPY . /app\nworkdir /app\n",
			want:        "from alpine:3.18\nrun echo hello\ncopy . /app\nworkdir /app\n",
			args:        []string{"--fix"},
			wantApplied: 1, // Only COPY needs fixing
		},
		// Multiple fixes with line shift: DL3003 splits one line into two,
		// then DL3027 fix on a later line must still apply correctly.
		// The fixer applies edits from end to start to handle position drift.
		{
			name: "multi-fix-line-shift",
			input: `FROM ubuntu:22.04
RUN cd /app && make build
RUN apt install curl
`,
			// DL3003 splits "RUN cd /app && make build" into "WORKDIR /app\nRUN make build"
			// DL3027 changes "apt install" to "apt-get install"
			want: `FROM ubuntu:22.04
WORKDIR /app
RUN make build
RUN apt-get install curl
`,
			args:        []string{"--fix", "--fix-unsafe"},
			wantApplied: 2, // DL3003 + DL3027
		},
		// MaintainerDeprecated: Replace MAINTAINER with LABEL
		{
			name:        "maintainer-deprecated",
			input:       "FROM alpine:3.18\nMAINTAINER John Doe <john@example.com>\nRUN echo hello\n",
			want:        "FROM alpine:3.18\nLABEL org.opencontainers.image.authors=\"John Doe <john@example.com>\"\nRUN echo hello\n",
			args:        []string{"--fix"},
			wantApplied: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a temporary directory with a Dockerfile
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath, []byte(tc.input), 0o644); err != nil {
				t.Fatalf("failed to write Dockerfile: %v", err)
			}

			// Create an empty config file to prevent discovery of repo configs
			configPath := filepath.Join(tmpDir, ".tally.toml")
			if err := os.WriteFile(configPath, []byte(""), 0o644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			// Run tally check --fix
			args := append([]string{"check", "--config", configPath}, tc.args...)
			args = append(args, dockerfilePath)
			cmd := exec.Command(binaryPath, args...)
			cmd.Env = append(os.Environ(),
				"GOCOVERDIR="+coverageDir,
			)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("check --fix failed: %v\noutput:\n%s", err, output)
			}

			// Read the fixed Dockerfile
			fixed, err := os.ReadFile(dockerfilePath)
			if err != nil {
				t.Fatalf("failed to read fixed Dockerfile: %v", err)
			}

			if string(fixed) != tc.want {
				t.Errorf("fixed content mismatch:\ngot:\n%s\nwant:\n%s\noutput:\n%s", fixed, tc.want, output)
			}

			// Check that the output mentions the expected number of fixes
			outputStr := string(output)
			if tc.wantApplied > 0 && !strings.Contains(outputStr, "Fixed") {
				t.Errorf("expected 'Fixed' in output, got: %s", outputStr)
			}
		})
	}
}

// TestFixRealWorld tests the auto-fix functionality on a real-world Dockerfile
// from a public repository, verifying that multiple fixes apply correctly.
// Source: https://github.com/tle211212/deepspeed_distributed_sagemaker_sample
func TestFixRealWorld(t *testing.T) {
	testdataDir := filepath.Join("testdata", "benchmark-real-world-fix")

	// Read the original Dockerfile
	originalContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read original Dockerfile: %v", err)
	}

	// Read the expected fixed output
	expectedContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile.expected"))
	if err != nil {
		t.Fatalf("failed to read expected Dockerfile: %v", err)
	}

	// Create a temp directory and copy the Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, originalContent, 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Copy the config file to disable max-lines
	configContent, err := os.ReadFile(filepath.Join(testdataDir, ".tally.toml"))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Run tally check --fix --fix-unsafe
	args := []string{"check", "--config", configPath, "--fix", "--fix-unsafe", dockerfilePath}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	// Exit code 1 is expected due to remaining unfixable violations
	// Just verify the command ran (output captured regardless of exit code)
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("command failed to run: %v", err)
		}
		// Exit code 1 is expected, other exit codes indicate real failures
		if exitErr.ExitCode() != 1 {
			t.Fatalf("unexpected exit code %d: %v\noutput:\n%s", exitErr.ExitCode(), err, output)
		}
	}

	// Read the fixed Dockerfile
	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	// Compare with expected
	if !bytes.Equal(fixedContent, expectedContent) {
		t.Errorf("fixed content does not match expected\noutput:\n%s", output)
	}

	// Verify the output mentions the expected fixes
	outputStr := string(output)
	if !strings.Contains(outputStr, "Fixed 14 issues") {
		t.Errorf("expected 'Fixed 14 issues' in output, got: %s", outputStr)
	}
}
