package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

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

	snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

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

	snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixed))

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

	snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixed))

	outputStr := string(output)
	if !strings.Contains(outputStr, "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", outputStr)
	}
}

// TestFixPreferAddGitCombined verifies that prefer-add-git owns RUN chains that
// would otherwise overlap with generic RUN-formatting rules. Neighboring
// whitespace rules still report on the original text surface, so this scenario
// expects a non-zero exit while confirming the git-source fixes won the edit race.
func TestFixPreferAddGitCombined(t *testing.T) {
	t.Parallel()

	input := "FROM alpine:3.20\n" +
		"ARG BRANCH_OFI=v1.6.0\n" +
		"RUN echo foo  && \\\n" +
		"    git clone https://github.com/NVIDIA/apex && cd apex && git checkout 0123456789abcdef0123456789abcdef01234567 && echo zoo  \n" +
		"RUN git clone https://github.com/aws/aws-ofi-nccl.git -b v${BRANCH_OFI}\n"

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(input), 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `[rules.tally.consistent-indentation]
severity = "style"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"lint", "--config", configPath, "--slow-checks=off",
		"--fix", "--fix-unsafe",
		"--ignore", "*",
		"--select", "tally/prefer-add-git",
		"--select", "tally/prefer-run-heredoc",
		"--select", "tally/newline-per-chained-call",
		"--select", "hadolint/DL3003",
		"--select", "tally/no-multi-spaces",
		"--select", "tally/no-trailing-spaces",
		"--select", "tally/consistent-indentation",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	expectExitCode1(t, output, err)

	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

	gotApplied, ok, err := parseFixedCount(string(output))
	if err != nil {
		t.Fatalf("failed to parse fixed summary: %v\noutput:\n%s", err, output)
	}
	if !ok {
		t.Fatalf("expected fixed summary in output, got:\n%s", output)
	}
	if gotApplied != 2 {
		t.Fatalf("expected 2 fixes applied, got %d\noutput:\n%s", gotApplied, output)
	}
}

// TestFixTelemetryOptOutCombined runs the telemetry rule alongside the related
// content, shell, heredoc, and formatting rules that can touch the same stages.
func TestFixTelemetryOptOutCombined(t *testing.T) {
	t.Parallel()
	testdataDir := filepath.Join("testdata", "telemetry-opt-out-combined")

	dockerfileContent, err := os.ReadFile(filepath.Join(testdataDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	packageJSON, err := os.ReadFile(filepath.Join(testdataDir, "package.json"))
	if err != nil {
		t.Fatalf("failed to read package.json fixture: %v", err)
	}

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, dockerfileContent, 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), packageJSON, 0o644); err != nil {
		t.Fatalf("failed to write package.json: %v", err)
	}

	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := `[rules.tally.prefer-telemetry-opt-out]
severity = "info"

[rules.tally.prefer-package-cache-mounts]
severity = "info"

[rules.tally.prefer-run-heredoc]
severity = "style"

[rules.tally.consistent-indentation]
severity = "style"

[rules.tally.newline-between-instructions]
severity = "style"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"lint", "--config", configPath, "--context", tmpDir, "--slow-checks=off",
		"--fix", "--fix-unsafe",
		"--ignore", "*",
		"--select", "tally/prefer-telemetry-opt-out",
		"--select", "tally/prefer-curl-config",
		"--select", "tally/prefer-package-cache-mounts",
		"--select", "tally/powershell/prefer-shell-instruction",
		"--select", "hadolint/DL4006",
		"--select", "tally/prefer-run-heredoc",
		"--select", "tally/newline-between-instructions",
		"--select", "tally/consistent-indentation",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected exit code 0 (all selected violations fixable), got error: %v\noutput: %s", err, output)
	}

	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
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

	snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixed))

	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}

// TestFixCrossRuleMultiSpacesIndentationChain verifies that three rules targeting
// the same RUN instruction cooperate correctly:
//
//   - tally/no-multi-spaces        (priority 10) — removes extra spaces
//   - tally/consistent-indentation (priority 50) — adds tab indentation for non-first stages
//   - tally/newline-per-chained-call (priority 97) — splits chained commands onto separate lines
//
// The fixture is a multi-stage Dockerfile where the second stage has a single-line
// RUN with chained commands AND multiple consecutive spaces.  All three rules must
// apply their fixes without corrupting backslash continuations, indentation, or
// quoted strings.
func TestFixCrossRuleMultiSpacesIndentationChain(t *testing.T) {
	t.Parallel()

	// Second-stage RUN deliberately has:
	//  - double spaces after "apt-get" and around flags  → no-multi-spaces
	//  - chained "&&" on one line                        → newline-per-chained-call
	//  - no tab indentation                              → consistent-indentation
	//  - a quoted string with significant inner spaces   → must NOT be touched
	input := "FROM golang:1.23 AS build\n" +
		"\tRUN go build -o /app ./...\n" +
		"\n" +
		"FROM debian:bookworm-slim\n" +
		"COPY --from=build /app /usr/local/bin/app\n" +
		"RUN apt-get  update &&  apt-get install -y  --no-install-recommends  ca-certificates && rm -rf  /var/lib/apt/lists/*\n" +
		"RUN echo \"    keepspaces\" > /etc/motd\n" +
		"CMD [\"app\"]\n"

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(input), 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	// Enable consistent-indentation (experimental, off by default).
	configPath := filepath.Join(tmpDir, ".tally.toml")
	configContent := "[rules.tally.consistent-indentation]\nseverity = \"style\"\n"
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	args := []string{
		"lint", "--config", configPath, "--slow-checks=off",
		"--fix",
		"--ignore", "*",
		"--select", "tally/no-multi-spaces",
		"--select", "tally/consistent-indentation",
		"--select", "tally/newline-per-chained-call",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("command failed to run: %v\noutput:\n%s", err, output)
		}
	}

	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}

// TestFixCrossRuleCopyChmodHeredoc verifies that COPY-related rules cooperate
// when applied to the same Dockerfile:
//
//   - tally/prefer-copy-chmod   — merges COPY + RUN chmod into COPY --chmod
//   - tally/prefer-copy-heredoc — replaces RUN echo/cat > file with COPY <<EOF
//
// The fixture mixes patterns owned by each rule in the same stage.  Both rules
// must apply their fixes without conflicts.
func TestFixCrossRuleCopyChmodHeredoc(t *testing.T) {
	t.Parallel()

	// Patterns exercised:
	//   Line 3-4:  COPY + RUN chmod +x  → prefer-copy-chmod
	//   Line 5:    RUN echo > file      → prefer-copy-heredoc
	//   Line 6-7:  COPY --chown + chmod → prefer-copy-chmod (with existing flag)
	//   Line 8-11: RUN cat heredoc      → prefer-copy-heredoc
	input := "FROM python:3.12-slim\n" +
		"WORKDIR /app\n" +
		"COPY entrypoint.sh /app/entrypoint.sh\n" +
		"RUN chmod +x /app/entrypoint.sh\n" +
		"RUN echo 'APP_ENV=production' > /app/.env\n" +
		"COPY --chown=app:app healthcheck.sh /app/healthcheck.sh\n" +
		"RUN chmod 755 /app/healthcheck.sh\n" +
		"RUN cat <<'CONF' > /etc/app.conf\n" +
		"log_level = info\n" +
		"workers = 4\n" +
		"CONF\n" +
		"ENTRYPOINT [\"/app/entrypoint.sh\"]\n"

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(input), 0o644); err != nil {
		t.Fatalf("failed to write Dockerfile: %v", err)
	}

	args := []string{
		"lint", "--slow-checks=off",
		"--fix", "--fix-unsafe",
		"--ignore", "*",
		"--select", "tally/prefer-copy-chmod",
		"--select", "tally/prefer-copy-heredoc",
		dockerfilePath,
	}
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("command failed to run: %v\noutput:\n%s", err, output)
		}
	}

	fixedContent, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read fixed Dockerfile: %v", err)
	}

	snaps.WithConfig(snaps.Raw(), snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, string(fixedContent))

	if !strings.Contains(string(output), "Fixed") {
		t.Errorf("expected 'Fixed' in output, got: %s", output)
	}
}
