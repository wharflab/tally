package integration

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLintErrors verifies tally's behavior with files that cannot be linted normally:
// unparseable content, binary files, non-existent paths, and permission errors.
func TestLintErrors(t *testing.T) {
	t.Parallel()

	t.Run("unparseable-json-file", testLintErrorUnparseableJSON)
	t.Run("binary-file", testLintErrorBinaryFile)
	t.Run("nonexistent-file", testLintErrorNonexistentFile)
	t.Run("nonexistent-glob", testLintErrorNonexistentGlob)
	t.Run("empty-directory", testLintErrorEmptyDirectory)
	t.Run("permission-denied", testLintErrorPermissionDenied)
}

func testLintErrorUnparseableJSON(t *testing.T) {
	t.Parallel()

	target := filepath.Join("testdata", "error-unparseable-json", "Dockerfile")
	stdout, stderr, exitCode := runTallyLintRaw(t, target)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	// BuildKit's parser is lenient — it accepts arbitrary text as AST nodes.
	// A JSON file parses into garbled instructions without a real FROM,
	// so the linter produces DL3061 violations for every line and exits 1.
	if exitCode != 1 {
		t.Errorf("expected exit code 1 (violations), got %d", exitCode)
	}
	if !strings.Contains(stdout, "DL3061") {
		t.Error("expected DL3061 violation for non-Dockerfile content")
	}
	if !strings.Contains(stdout, "Invalid instruction order") {
		t.Error("expected 'Invalid instruction order' message")
	}
}

func testLintErrorBinaryFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binFile := filepath.Join(tmpDir, "Dockerfile")

	// 256 KB of random binary content.
	data := make([]byte, 256*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("generate random data: %v", err)
	}
	if err := os.WriteFile(binFile, data, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}

	stdout, stderr, exitCode := runTallyLintRaw(t, binFile)

	t.Logf("exit=%d\nstdout length=%d\nstderr:\n%s", exitCode, len(stdout), stderr)

	// Binary content parses into garbled AST nodes, but the JSON reporter
	// fails when serializing source snippets with invalid UTF-8.
	if exitCode != 2 {
		t.Errorf("expected exit code 2 (config error from invalid UTF-8), got %d", exitCode)
	}
	if !strings.Contains(stderr, "invalid UTF-8") {
		t.Errorf("expected 'invalid UTF-8' in stderr, got: %q", stderr)
	}
}

func testLintErrorNonexistentFile(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "this-file-does-not-exist.Dockerfile")
	stdout, stderr, exitCode := runTallyLintRaw(t, target)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 3 {
		t.Errorf("expected exit code 3 (no files), got %d", exitCode)
	}
	if !strings.Contains(stderr, "file not found:") {
		t.Errorf("expected 'file not found:' in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for nonexistent file, got: %q", stdout)
	}
}

func testLintErrorNonexistentGlob(t *testing.T) {
	t.Parallel()

	// A glob pattern that matches nothing.
	target := filepath.Join(t.TempDir(), "*.NoSuchDockerfile")
	stdout, stderr, exitCode := runTallyLintRaw(t, target)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 3 {
		t.Errorf("expected exit code 3 (no files), got %d", exitCode)
	}
	if !strings.Contains(stderr, "no Dockerfiles matched pattern:") {
		t.Errorf("expected 'no Dockerfiles matched pattern:' in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for empty glob, got: %q", stdout)
	}
}

func testLintErrorEmptyDirectory(t *testing.T) {
	t.Parallel()

	// A directory that exists but contains no Dockerfiles.
	emptyDir := t.TempDir()
	stdout, stderr, exitCode := runTallyLintRaw(t, emptyDir)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 3 {
		t.Errorf("expected exit code 3 (no files), got %d", exitCode)
	}
	if !strings.Contains(stderr, "no Dockerfile or Containerfile found in") {
		t.Errorf("expected directory-specific message in stderr, got: %q", stderr)
	}
	// Message should contain the absolute path.
	if !strings.Contains(stderr, emptyDir) {
		t.Errorf("expected absolute path %q in stderr, got: %q", emptyDir, stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for empty directory, got: %q", stdout)
	}
}

func testLintErrorPermissionDenied(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission tests not reliable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("cannot test permission denial as root")
	}

	tmpDir := t.TempDir()
	noRead := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(noRead, []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Chmod(noRead, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(noRead, 0o600); err != nil {
			t.Logf("cleanup: restore permissions: %v", err)
		}
	})

	stdout, stderr, exitCode := runTallyLintRaw(t, noRead)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 2 {
		t.Errorf("expected exit code 2 (config error), got %d", exitCode)
	}
	if !strings.Contains(stderr, "permission denied") {
		t.Errorf("expected 'permission denied' in stderr, got: %q", stderr)
	}
	if !strings.Contains(stderr, "failed to lint") {
		t.Errorf("expected 'failed to lint' prefix in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for permission denied, got: %q", stdout)
	}
}

// runTallyLintRaw runs the tally binary with the lint subcommand and returns
// raw stdout, stderr, and exit code.
func runTallyLintRaw(t *testing.T, target string) (string, string, int) {
	t.Helper()

	cmd := exec.Command(binaryPath, "lint", "--format", "json", "--slow-checks=off", target)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	var exitCode int
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("command failed to start: %v", err)
		}
	}

	return outBuf.String(), errBuf.String(), exitCode
}
