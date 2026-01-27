package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

	binaryPath = filepath.Join(tmpDir, "tally")

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
		name     string
		dir      string
		args     []string
		env      []string
		wantExit int
	}{
		// Basic tests
		{"simple", "simple", []string{"--format", "json"}, nil, 0},
		{"simple-max-lines-pass", "simple", []string{"--max-lines", "100", "--format", "json"}, nil, 0},
		{"simple-max-lines-fail", "simple", []string{"--max-lines", "2", "--format", "json"}, nil, 1},

		// Config file discovery tests
		{"config-file-discovery", "with-config", nil, nil, 1},
		{"config-cascading-discovery", "nested/subdir", nil, nil, 1},
		{"config-skip-options", "with-blanks-and-comments", nil, nil, 0},
		{"cli-overrides-config", "with-config", []string{"--max-lines", "100"}, nil, 0},

		// Environment variable tests
		{
			"env-var-override", "simple",
			[]string{"--format", "json"},
			[]string{"TALLY_RULES_MAX_LINES_MAX=2"},
			1,
		},

		// BuildKit linter warnings tests
		// These test that BuildKit's built-in warnings are captured and surfaced
		{"buildkit-warnings", "buildkit-warnings", []string{"--format", "json"}, nil, 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dockerfilePath := filepath.Join("testdata", tc.dir, "Dockerfile")

			args := make([]string, 0, 1+len(tc.args)+1)
			args = append(args, "check")
			args = append(args, tc.args...)
			args = append(args, dockerfilePath)
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
				t.Errorf("expected exit code %d, got %d", tc.wantExit, exitCode)
			}

			snaps.WithConfig(snaps.Ext(".json")).MatchStandaloneSnapshot(t, string(output))
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
