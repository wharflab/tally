package integration

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
)

const fixtureRoot = "fixtures"

func TestLintFixtures(t *testing.T) {
	t.Parallel()

	for _, dir := range fixtureDirs(t, filepath.Join(fixtureRoot, "lint")) {
		tc := dir
		t.Run(filepath.Base(tc), func(t *testing.T) {
			t.Parallel()
			runLintFixture(t, tc)
		})
	}
}

func TestFixFixtures(t *testing.T) {
	t.Parallel()

	for _, dir := range fixtureDirs(t, filepath.Join(fixtureRoot, "fix")) {
		tc := dir
		t.Run(filepath.Base(tc), func(t *testing.T) {
			t.Parallel()
			runFixFixture(t, tc)
		})
	}
}

func fixtureDirs(t *testing.T, root string) []string {
	t.Helper()

	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read fixture root %s: %v", root, err)
	}

	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if !fileExists(filepath.Join(dir, "Dockerfile")) {
			continue
		}
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	return dirs
}

func runLintFixture(t *testing.T, dir string) {
	t.Helper()

	dockerfilePath := filepath.Join(dir, "Dockerfile")
	args := []string{"lint", "--format", "json"}
	if !fileExists(filepath.Join(dir, ".tally.toml")) && !fileExists(filepath.Join(dir, "tally.toml")) {
		args = append(args, "--no-config")
	}
	args = append(args, dockerfilePath)
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	exitCode := commandExitCode(t, err, stdoutBuf.String(), stderrBuf.String())
	if exitCode != 0 && exitCode != 1 {
		t.Fatalf("unexpected exit code %d\nstdout:\n%s\nstderr:\n%s", exitCode, stdoutBuf.String(), stderrBuf.String())
	}
	if stderrBuf.Len() > 0 {
		t.Fatalf("unexpected stderr:\n%s", stderrBuf.String())
	}

	got := normalizeFixtureOutput(t, stdoutBuf.String())
	snaps.WithConfig(
		snaps.Dir(dir),
		snaps.Filename("result"),
		snaps.JSON(snaps.JSONConfig{Indent: "  ", SortKeys: true}),
	).MatchStandaloneJSON(t, got)
}

func runFixFixture(t *testing.T, dir string) {
	t.Helper()

	input := readFixtureFile(t, filepath.Join(dir, "Dockerfile"))
	args := []string{"lint", "--format", "markdown", "--fix"}
	if !fileExists(filepath.Join(dir, ".tally.toml")) && !fileExists(filepath.Join(dir, "tally.toml")) {
		args = append(args, "--no-config")
	}
	args = append(args, "-")
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)
	cmd.Stdin = strings.NewReader(input)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	exitCode := commandExitCode(t, err, stdoutBuf.String(), stderrBuf.String())
	if exitCode != 0 && exitCode != 1 {
		t.Fatalf("unexpected exit code %d\nstdout:\n%s\nstderr:\n%s", exitCode, stdoutBuf.String(), stderrBuf.String())
	}

	gotFixed := normalizeFixtureOutput(t, stdoutBuf.String())
	snaps.WithConfig(
		snaps.Dir(dir),
		snaps.Filename("fixed"),
		snaps.Ext(".Dockerfile"),
		snaps.Raw(),
	).MatchStandaloneSnapshot(t, gotFixed)

	if stderrBuf.Len() == 0 && !fixtureSnapshotExists(t, dir, "report", ".md") {
		return
	}
	gotReport := normalizeFixtureOutput(t, stderrBuf.String())
	snaps.WithConfig(
		snaps.Dir(dir),
		snaps.Filename("report"),
		snaps.Ext(".md"),
		snaps.Raw(),
	).MatchStandaloneSnapshot(t, gotReport)
}

func commandExitCode(t *testing.T, err error, stdout, stderr string) int {
	t.Helper()
	if err == nil {
		return 0
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	t.Fatalf("command failed to start: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	return 0
}

func normalizeFixtureOutput(t *testing.T, output string) string {
	t.Helper()
	output = strings.ReplaceAll(output, "\r\n", "\n")
	wd, err := os.Getwd()
	if err == nil {
		output = strings.ReplaceAll(output, filepath.ToSlash(wd)+"/", "")
	}
	return buildkitVersionRE.ReplaceAllString(output, "${1}0.0.0")
}

func readFixtureFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fixtureSnapshotExists(t *testing.T, dir, filename, ext string) bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, filename+"_*.snap"+ext))
	if err != nil {
		t.Fatalf("glob fixture snapshots: %v", err)
	}
	return len(matches) > 0
}
