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
// unparseable content, binary files, non-existent paths, permission errors, and syntax errors.
func TestLintErrors(t *testing.T) {
	t.Parallel()

	t.Run("unparseable-json-file", testLintErrorUnparseableJSON)
	t.Run("too-small-file", testLintErrorTooSmallFile)
	t.Run("binary-file", testLintErrorBinaryFile)
	t.Run("large-file", testLintErrorLargeFile)
	t.Run("executable-dockerfile", testLintErrorExecutableDockerfile)
	t.Run("nonexistent-file", testLintErrorNonexistentFile)
	t.Run("nonexistent-glob", testLintErrorNonexistentGlob)
	t.Run("empty-directory", testLintErrorEmptyDirectory)
	t.Run("permission-denied", testLintErrorPermissionDenied)
	t.Run("unknown-instruction", testLintErrorUnknownInstruction)
	t.Run("syntax-directive-typo", testLintErrorSyntaxDirectiveTypo)
	t.Run("multiple-unknown-instructions", testLintErrorMultipleUnknownInstructions)
}

func testLintErrorUnparseableJSON(t *testing.T) {
	t.Parallel()

	target := filepath.Join("testdata", "error-unparseable-json", "Dockerfile")
	stdout, stderr, exitCode := runTallyLintRaw(t, target)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	// BuildKit's parser is lenient — it accepts arbitrary text as AST nodes.
	// A JSON file parses into garbled instructions without a real FROM.
	// The fail-fast syntax checks now catch this before the lint pipeline,
	// reporting unknown instructions via stderr and returning exit code 4.
	if exitCode != 4 {
		t.Errorf("expected exit code 4 (syntax error), got %d", exitCode)
	}
	if !strings.Contains(stderr, "unknown instruction") {
		t.Error("expected 'unknown instruction' in stderr for non-Dockerfile content")
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for syntax error, got: %q", stdout)
	}
}

func testLintErrorTooSmallFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tinyFile := filepath.Join(tmpDir, "Dockerfile")
	// 3 bytes — well below the 6-byte minimum ("FROM a").
	if err := os.WriteFile(tinyFile, []byte("FR\n"), 0o644); err != nil {
		t.Fatalf("write tiny file: %v", err)
	}

	stdout, stderr, exitCode := runTallyLintRaw(t, tinyFile)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 2 {
		t.Errorf("expected exit code 2 (config error from file validation), got %d", exitCode)
	}
	if !strings.Contains(stderr, "too small for a valid Dockerfile") {
		t.Errorf("expected 'too small for a valid Dockerfile' in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for too-small file, got: %q", stdout)
	}
}

func testLintErrorBinaryFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	binFile := filepath.Join(tmpDir, "Dockerfile")

	// 50 KB of random binary content (below the default 100 KB size limit).
	data := make([]byte, 50*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("generate random data: %v", err)
	}
	if err := os.WriteFile(binFile, data, 0o644); err != nil {
		t.Fatalf("write binary file: %v", err)
	}

	stdout, stderr, exitCode := runTallyLintRaw(t, binFile)

	t.Logf("exit=%d\nstdout length=%d\nstderr:\n%s", exitCode, len(stdout), stderr)

	// Pre-parse file validation now catches binary files before BuildKit parsing.
	if exitCode != 2 {
		t.Errorf("expected exit code 2 (config error from file validation), got %d", exitCode)
	}
	if !strings.Contains(stderr, "valid UTF-8") {
		t.Errorf("expected 'valid UTF-8' in stderr, got: %q", stderr)
	}
}

func testLintErrorLargeFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	largeFile := filepath.Join(tmpDir, "Dockerfile")

	// 200 KB file — exceeds the default 100 KB limit.
	data := make([]byte, 200*1024)
	for i := range data {
		data[i] = 'A'
	}
	if err := os.WriteFile(largeFile, data, 0o644); err != nil {
		t.Fatalf("write large file: %v", err)
	}

	stdout, stderr, exitCode := runTallyLintRaw(t, largeFile)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 2 {
		t.Errorf("expected exit code 2 (config error from file validation), got %d", exitCode)
	}
	if !strings.Contains(stderr, "file too large") {
		t.Errorf("expected 'file too large' in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for large file, got: %q", stdout)
	}
}

func testLintErrorExecutableDockerfile(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("executable-bit check not applicable on Windows")
	}

	tmpDir := t.TempDir()
	execFile := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(execFile, []byte("FROM alpine\n"), 0o755); err != nil {
		t.Fatalf("write executable file: %v", err)
	}

	stdout, stderr, exitCode := runTallyLintRaw(t, execFile)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 2 {
		t.Errorf("expected exit code 2 (config error from file validation), got %d", exitCode)
	}
	if !strings.Contains(stderr, "unexpected executable") {
		t.Errorf("expected 'unexpected executable' in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for executable dockerfile, got: %q", stdout)
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

func testLintErrorUnknownInstruction(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte("FORM alpine\nRUN echo hello\n"), 0o644); err != nil {
		t.Fatalf("write dockerfile: %v", err)
	}

	stdout, stderr, exitCode := runTallyLintRaw(t, dockerfilePath)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 4 {
		t.Errorf("expected exit code 4 (syntax error), got %d", exitCode)
	}
	if !strings.Contains(stderr, `unknown instruction "FORM"`) {
		t.Errorf("expected 'unknown instruction \"FORM\"' in stderr, got: %q", stderr)
	}
	if !strings.Contains(stderr, `did you mean "FROM"`) {
		t.Errorf("expected 'did you mean \"FROM\"' in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for syntax error, got: %q", stdout)
	}
}

func testLintErrorSyntaxDirectiveTypo(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	content := "# syntax=docker/dokcerfile:1.7\nFROM alpine\n"
	if err := os.WriteFile(dockerfilePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write dockerfile: %v", err)
	}

	stdout, stderr, exitCode := runTallyLintRaw(t, dockerfilePath)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 4 {
		t.Errorf("expected exit code 4 (syntax error), got %d", exitCode)
	}
	if !strings.Contains(stderr, "syntax directive") {
		t.Errorf("expected 'syntax directive' in stderr, got: %q", stderr)
	}
	if !strings.Contains(stderr, "did you mean") {
		t.Errorf("expected 'did you mean' in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for syntax error, got: %q", stdout)
	}
}

func testLintErrorMultipleUnknownInstructions(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	content := "FORM alpine\nCOPPY . /app\nRUNN echo hello\n"
	if err := os.WriteFile(dockerfilePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write dockerfile: %v", err)
	}

	stdout, stderr, exitCode := runTallyLintRaw(t, dockerfilePath)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 4 {
		t.Errorf("expected exit code 4 (syntax error), got %d", exitCode)
	}
	// All three typos should be reported.
	if !strings.Contains(stderr, "FORM") {
		t.Errorf("expected 'FORM' in stderr, got: %q", stderr)
	}
	if !strings.Contains(stderr, "COPPY") {
		t.Errorf("expected 'COPPY' in stderr, got: %q", stderr)
	}
	if !strings.Contains(stderr, "RUNN") {
		t.Errorf("expected 'RUNN' in stderr, got: %q", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for syntax error, got: %q", stdout)
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
