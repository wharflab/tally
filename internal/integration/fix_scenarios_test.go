package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
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

	// Run tally check --fix --fix-unsafe (all rules enabled, slow checks off)
	args := []string{"check", "--config", configPath, "--slow-checks=off", "--fix", "--fix-unsafe", dockerfilePath}
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
	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

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
		"check", "--config", configPath, "--slow-checks=off",
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

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

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
		"check", "--config", configPath, "--slow-checks=off",
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

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

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
		"check", "--config", configPath, "--slow-checks=off",
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

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixed))

	outputStr := string(output)
	if !strings.Contains(outputStr, "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", outputStr)
	}
}
