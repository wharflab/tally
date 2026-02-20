package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/testutil"
)

// TestFixRealWorld tests the auto-fix functionality on a real-world Dockerfile
// from a public repository, verifying that multiple fixes apply correctly.
// Source: https://github.com/tle211212/deepspeed_distributed_sagemaker_sample
func TestFixRealWorld(t *testing.T) {
	t.Parallel()
	testdataDir := filepath.Join("testdata", "benchmark-real-world-fix")

	// Read the original Dockerfile
	originalContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read original Dockerfile: %v", err)
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

	// Run tally lint --fix --fix-unsafe (all rules enabled, slow checks off)
	args := []string{"lint", "--config", configPath, "--slow-checks=off", "--fix", "--fix-unsafe", dockerfilePath}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	// Exit code 1 is expected due to remaining unfixable violations
	expectExitCode1(t, output, err)

	// Read the fixed Dockerfile
	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	// Use snapshot testing for easier maintenance
	testutil.MatchDockerfileSnapshot(t, string(fixedContent))

	// Verify that fixes were applied (check output contains "Fixed")
	outputStr := string(output)
	if !strings.Contains(outputStr, "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", outputStr)
	}
}

// TestFixHeredocCombined tests auto-fix with both prefer-copy-heredoc and prefer-run-heredoc
// enabled together on a multi-stage Dockerfile that also has consistent-indentation enabled.
// The snapshot makes it easy to review the final fixed Dockerfile.
func TestFixHeredocCombined(t *testing.T) {
	t.Parallel()
	testdataDir := filepath.Join("testdata", "heredoc-combined")

	// Read the original Dockerfile
	originalContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read original Dockerfile: %v", err)
	}

	// Create a temp directory and copy the Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, originalContent, 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Config: enable consistent-indentation (off by default)
	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `[rules.tally.consistent-indentation]
severity = "style"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Run with all three rules: consistent-indentation (50), prefer-copy-heredoc (99), prefer-run-heredoc (100)
	args := []string{
		"lint", "--config", configPath, "--slow-checks=off",
		"--fix", "--fix-unsafe",
		"--ignore", "*",
		"--select", "tally/consistent-indentation",
		"--select", "tally/prefer-copy-heredoc",
		"--select", "tally/prefer-run-heredoc",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	expectExitCode1(t, output, err)

	// Read the fixed Dockerfile and snapshot it
	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	testutil.MatchDockerfileSnapshot(t, string(fixedContent))

	// Verify fixes were applied
	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}

// TestFixConsistentIndentation tests auto-fix for the consistent-indentation rule
// on a multi-stage Dockerfile with multi-line continuation instructions.
// The snapshot makes it easy to verify all continuation lines get aligned.
func TestFixConsistentIndentation(t *testing.T) {
	t.Parallel()
	testdataDir := filepath.Join("testdata", "consistent-indentation")

	originalContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read original Dockerfile: %v", err)
	}

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, originalContent, 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Copy the config that enables consistent-indentation
	configContent, err := os.ReadFile(filepath.Join(testdataDir, ".tally.toml"))
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"lint", "--config", configPath, "--slow-checks=off",
		"--fix",
		"--select", "tally/consistent-indentation",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GOCOVERDIR="+coverageDir,
	)
	output, err := cmd.CombinedOutput()
	expectExitCode1(t, output, err)

	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	testutil.MatchDockerfileSnapshot(t, string(fixedContent))

	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}

// TestFixNewlineNeverWithInvalidDefinitionDescription tests cross-rule interaction between
// buildkit/InvalidDefinitionDescription (sync fix, inserts blank lines between comments
// and instructions) and tally/newline-between-instructions in "never" mode (async fix,
// removes blank lines between instructions). The sync fix runs first; the async resolver
// then re-parses the modified content and removes inter-instruction blank lines while
// preserving the blank lines inserted between comments and instructions.
func TestFixNewlineNeverWithInvalidDefinitionDescription(t *testing.T) {
	t.Parallel()

	// Dockerfile where:
	// - "# bad comment" doesn't match ARG name "foo" → InvalidDefinitionDescription violation
	// - blank lines between ARG→FROM and FROM→RUN → newline-between-instructions (never) violations
	input := "# bad comment\nARG foo=bar\n\nFROM scratch AS base\n\nRUN echo hello\n"

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(input), 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Enable InvalidDefinitionDescription (experimental, off by default)
	// and set newline-between-instructions to "never" mode.
	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `[rules.buildkit.InvalidDefinitionDescription]
severity = "info"

[rules.tally.newline-between-instructions]
mode = "never"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"lint", "--config", configPath, "--slow-checks=off",
		"--fix",
		"--ignore", "*",
		"--select", "buildkit/InvalidDefinitionDescription",
		"--select", "tally/newline-between-instructions",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	expectExitCode1(t, output, err)

	fixed, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	testutil.MatchDockerfileSnapshot(t, string(fixed))

	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}

// TestFixPreferAddUnpackBeatsHeredoc verifies that prefer-add-unpack (sync fix, priority 95)
// takes priority over prefer-run-heredoc (async fix, priority 100) when both rules target the
// same consecutive RUN instructions. After fixing, all RUNs become ADD --unpack.
// The prefer-run-heredoc violation still reports (exit code 1) since its fix was superseded.
func TestFixPreferAddUnpackBeatsHeredoc(t *testing.T) {
	t.Parallel()

	input := "FROM ubuntu:22.04\n" +
		"RUN curl -fsSL https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | tar -xz -C /usr/local\n" +
		"RUN curl -fsSL https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz | tar -xJ -C /usr/local\n" +
		"RUN curl -fsSL https://example.com/app.tar.gz | tar -xz -C /opt\n"

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(input), 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}
	// Write an empty config in tmpDir to prevent parent-directory config discovery
	// and keep this scenario isolated to only explicitly selected flags/rules.
	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"lint", "--config", configPath, "--slow-checks=off",
		"--fix-unsafe", "--fix",
		"--select", "tally/prefer-add-unpack",
		"--select", "tally/prefer-run-heredoc",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	// Exit code 1 expected: prefer-run-heredoc violation remains (fix superseded)
	expectExitCode1(t, output, err)

	fixed, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	testutil.MatchDockerfileSnapshot(t, string(fixed))

	outputStr := string(output)
	if !strings.Contains(outputStr, "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", outputStr)
	}
}

// TestFixNewlinePerChainedCall exercises the auto-fix path for
// tally/newline-per-chained-call end-to-end through the CLI.
// The rule is globally ignored in runFixCase (harness_test.go), so this test
// uses --ignore "*" + --select to run it in isolation.
func TestFixNewlinePerChainedCall(t *testing.T) {
	t.Parallel()

	input := "FROM alpine:3.20\n" +
		"RUN --mount=type=cache,target=/var/cache/apt " +
		"--mount=type=bind,source=go.sum,target=go.sum " +
		"apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*\n" +
		"LABEL org.opencontainers.image.title=myapp org.opencontainers.image.version=1.0\n" +
		"HEALTHCHECK CMD curl -f http://localhost/ || exit 1\n"

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(input), 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".tally.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"lint", "--config", configPath, "--slow-checks=off",
		"--fix",
		"--ignore", "*",
		"--select", "tally/newline-per-chained-call",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	// All violations are fixable (FixSafe) → exit code 0
	if err != nil {
		t.Fatalf("expected exit code 0 (all violations fixable), got error: %v\noutput: %s", err, output)
	}

	fixed, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	testutil.MatchDockerfileSnapshot(t, string(fixed))

	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}
