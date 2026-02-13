package integration

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

type checkCase struct {
	name       string
	dir        string
	args       []string
	env        []string
	wantExit   int
	snapExt    string // Snapshot file extension override (e.g. ".sarif", ".txt", ".md")
	snapRaw    bool   // If true, use MatchStandaloneSnapshot (plain text) instead of MatchStandaloneJSON
	isDir      bool   // If true, pass the directory instead of Dockerfile
	useContext bool   // If true, add --context flag for context-aware tests
	afterCheck func(t *testing.T, stderr string)
}

type fixCase struct {
	name        string
	input       string // Input Dockerfile content
	args        []string
	wantApplied int    // Expected number of fixes applied
	config      string // Optional config file content (empty string uses default empty config)
}

var fixedSummaryRE = regexp.MustCompile(`(?m)^Fixed (\d+) issues? in \d+ files?$`)

func runCheckCase(t *testing.T, tc checkCase) {
	t.Helper()

	testdataDir := filepath.Join("testdata", tc.dir)

	args := make([]string, 0, 1+len(tc.args)+4)
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
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	output := stdoutBuf.Bytes()

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
		t.Errorf("expected exit code %d, got %d\nstdout: %s\nstderr: %s", tc.wantExit, exitCode, output, stderrBuf.String())
	}

	// Normalize output for cross-platform snapshot comparison
	outputStr := string(output)
	// Normalize line endings (Windows CRLF -> LF) for consistent snapshots
	outputStr = strings.ReplaceAll(outputStr, "\r\n", "\n")

	// Replace absolute paths with relative ones for reproducible snapshots.
	wd, err := os.Getwd()
	if err == nil {
		wdSlash := filepath.ToSlash(wd) + "/"
		outputStr = strings.ReplaceAll(outputStr, wdSlash, "")
	}

	if tc.snapRaw {
		// Non-JSON formats (e.g. github-actions .txt, markdown .md)
		snaps.WithConfig(snaps.Ext(tc.snapExt)).MatchStandaloneSnapshot(t, outputStr)
	} else {
		// JSON output — MatchStandaloneJSON validates JSON and
		// defaults to .snap.json; Ext overrides for variants like .sarif.
		opts := []func(*snaps.Config){
			snaps.JSON(snaps.JSONConfig{
				SortKeys: true,
				Indent:   "  ",
			}),
		}
		if tc.snapExt != "" {
			opts = append(opts, snaps.Ext(tc.snapExt))
		}
		snaps.WithConfig(opts...).MatchStandaloneJSON(t, outputStr)
	}

	if tc.afterCheck != nil {
		tc.afterCheck(t, stderrBuf.String())
	}
}

func runFixCase(t *testing.T, tc fixCase) {
	t.Helper()

	// Create a temporary directory with a Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(tc.input), 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Create config file (custom content or empty to prevent discovery of repo configs)
	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, []byte(tc.config), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Run tally check --fix (disable slow checks — fix tests don't need async).
	// Ignore DL3057 (HEALTHCHECK missing) as it fires for nearly every fixture
	// and has no auto-fix; it's irrelevant for testing other fixes.
	args := append([]string{"check", "--config", configPath, "--slow-checks=off", "--ignore", "hadolint/DL3057"}, tc.args...)
	args = append(args, dockerfilePath)
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("check --fix command failed to run: %v\noutput:\n%s", err, output)
		}
		// Non-zero exits are valid for fix runs when unfixed violations remain.
	}

	// Read the fixed Dockerfile and snapshot it
	fixed, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixed))

	// Check that the output mentions the expected number of fixes
	outputStr := string(output)
	gotApplied, ok, err := parseFixedCount(outputStr)
	if tc.wantApplied > 0 {
		if err != nil {
			t.Fatalf("failed to parse fixed summary: %v\noutput:\n%s", err, outputStr)
		}
		if !ok {
			t.Fatalf("expected fixed summary in output, got:\n%s", outputStr)
		}
		if gotApplied != tc.wantApplied {
			t.Errorf("expected %d fixes applied, got %d\noutput:\n%s", tc.wantApplied, gotApplied, outputStr)
		}
		return
	}

	if err != nil {
		t.Fatalf("failed to parse fixed summary: %v\noutput:\n%s", err, outputStr)
	}
	if ok && gotApplied != 0 {
		t.Errorf("expected no fixes applied, got %d\noutput:\n%s", gotApplied, outputStr)
	}
}

func parseFixedCount(output string) (int, bool, error) {
	match := fixedSummaryRE.FindStringSubmatch(output)
	if len(match) == 0 {
		return 0, false, nil
	}
	count, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false, err
	}
	return count, true, nil
}

func expectExitCode1(t *testing.T, output []byte, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected exit code 1 but command succeeded\noutput:\n%s", output)
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("command failed to run: %v\noutput:\n%s", err, output)
	}

	if exitErr.ExitCode() != 1 {
		t.Fatalf("unexpected exit code %d: %v\noutput:\n%s", exitErr.ExitCode(), err, output)
	}
}
