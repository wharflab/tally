package integration

import (
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

func TestStdin(t *testing.T) {
	t.Parallel()

	t.Run("lint-json", testStdinLintJSON)
	t.Run("lint-text", testStdinLintText)
	t.Run("fix-outputs-to-stdout", testStdinFixOutputsToStdout)
	t.Run("fix-no-changes", testStdinFixNoChanges)
	t.Run("empty-stdin", testStdinEmpty)
	t.Run("mixed-with-files", testStdinMixedWithFiles)
	t.Run("syntax-error", testStdinSyntaxError)
}

// testStdinLintJSON verifies that linting from stdin produces JSON diagnostics
// on stdout with <stdin> as the file path.
func testStdinLintJSON(t *testing.T) {
	t.Parallel()

	input := "FROM ubuntu\nRUN apt-get update\n"
	stdout, stderr, exitCode := runTallyStdin(t, input,
		"lint", "--format", "json", "--slow-checks=off",
		"--ignore", "*",
		"--select", "hadolint/DL3006",
		"-",
	)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// Verify <stdin> appears as the file path (JSON encodes < > as \u003c \u003e).
	if !strings.Contains(stdout, "<stdin>") && !strings.Contains(stdout, `\u003cstdin\u003e`) {
		t.Errorf("expected <stdin> in output, got: %s", stdout)
	}

	snaps.WithConfig(
		snaps.JSON(snaps.JSONConfig{SortKeys: true, Indent: "  "}),
	).MatchStandaloneJSON(t, stdout)
}

// testStdinLintText verifies that text-format diagnostics work with stdin.
func testStdinLintText(t *testing.T) {
	t.Parallel()

	input := "FROM ubuntu\nRUN apt-get update\n"
	stdout, stderr, exitCode := runTallyStdin(t, input,
		"lint", "--no-color", "--slow-checks=off",
		"--ignore", "*",
		"--select", "hadolint/DL3006",
		"-",
	)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(stdout, "<stdin>") {
		t.Errorf("expected <stdin> in output, got: %s", stdout)
	}

	snaps.WithConfig(snaps.Ext(".txt")).MatchStandaloneSnapshot(t, stdout)
}

// testStdinFixOutputsToStdout verifies that --fix with stdin writes the fixed
// Dockerfile content to stdout. The test isolates a single fixable rule
// (newline-between-instructions) to get a predictable fix.
func testStdinFixOutputsToStdout(t *testing.T) {
	t.Parallel()

	// This input triggers newline-between-instructions (no blank line between
	// FROM and RUN).
	input := "FROM alpine:3.19\nRUN echo hello\n"
	stdout, stderr, exitCode := runTallyStdin(t, input,
		"lint", "--fix", "--slow-checks=off",
		"--ignore", "*",
		"--select", "tally/newline-between-instructions",
		"-",
	)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	// Fixed content should be on stdout.
	if !strings.Contains(stdout, "FROM alpine:3.19") {
		t.Errorf("expected fixed Dockerfile in stdout, got: %s", stdout)
	}

	// The fix should add a blank line between FROM and RUN.
	if !strings.Contains(stdout, "FROM alpine:3.19\n\nRUN echo hello") {
		t.Errorf("expected blank line between instructions in stdout, got: %s", stdout)
	}

	// Fix summary should be on stderr.
	if !strings.Contains(stderr, "Fixed") {
		t.Errorf("expected 'Fixed' in stderr, got: %s", stderr)
	}

	// Snapshot the fixed output.
	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, stdout)
}

// testStdinFixNoChanges verifies that --fix with stdin echoes back the original
// content when no fixes are applicable.
func testStdinFixNoChanges(t *testing.T) {
	t.Parallel()

	input := "FROM alpine:3.19\n\nRUN echo hello\n"
	stdout, _, exitCode := runTallyStdin(t, input,
		"lint", "--fix", "--slow-checks=off",
		"--ignore", "*",
		"--select", "tally/newline-between-instructions",
		"-",
	)

	// Stdout should contain the original content unchanged.
	if stdout != input {
		t.Errorf("expected original content on stdout\nwant: %q\ngot:  %q", input, stdout)
	}

	// Exit 0 because the only selected rule has no violations.
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

// testStdinEmpty verifies that empty stdin produces an error.
func testStdinEmpty(t *testing.T) {
	t.Parallel()

	stdout, stderr, exitCode := runTallyStdin(t, "",
		"lint", "-",
	)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 3 {
		t.Errorf("expected exit code 3 (no files), got %d", exitCode)
	}
	if !strings.Contains(stderr, "empty input from stdin") {
		t.Errorf("expected 'empty input from stdin' in stderr, got: %s", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout, got: %s", stdout)
	}
}

// testStdinMixedWithFiles verifies that combining - with file arguments errors.
func testStdinMixedWithFiles(t *testing.T) {
	t.Parallel()

	stdout, stderr, exitCode := runTallyStdin(t, "FROM alpine\n",
		"lint", "--", "-", "Dockerfile",
	)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 2 {
		t.Errorf("expected exit code 2 (config error), got %d", exitCode)
	}
	if !strings.Contains(stderr, "cannot mix stdin") {
		t.Errorf("expected 'cannot mix stdin' in stderr, got: %s", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout, got: %s", stdout)
	}
}

// testStdinSyntaxError verifies that syntax errors in stdin are reported properly.
func testStdinSyntaxError(t *testing.T) {
	t.Parallel()

	stdout, stderr, exitCode := runTallyStdin(t, "FORM alpine\nRUN echo hello\n",
		"lint", "-",
	)

	t.Logf("exit=%d\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)

	if exitCode != 4 {
		t.Errorf("expected exit code 4 (syntax error), got %d", exitCode)
	}
	if !strings.Contains(stderr, `unknown instruction "FORM"`) {
		t.Errorf("expected 'unknown instruction \"FORM\"' in stderr, got: %s", stderr)
	}
	if stdout != "" {
		t.Errorf("expected empty stdout for syntax error, got: %s", stdout)
	}
}
