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
	"github.com/pelletier/go-toml/v2"
)

const fixtureRoot = "fixtures"

// Registry-backed fixtures share one in-process mock registry. Keep them
// serial so race-enabled CI runs do not overload the resolver path and turn
// deterministic slow-check assertions into registry-unreachable noise.
var slowCheckFixtureSlots = make(chan struct{}, 1)

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
		if fixtureBuildFile(dir) == "" {
			continue
		}
		dirs = append(dirs, dir)
	}
	slices.Sort(dirs)
	return dirs
}

func runLintFixture(t *testing.T, dir string) {
	t.Helper()
	releaseSlowCheckSlot := acquireSlowCheckFixtureSlot(t, dir)
	defer releaseSlowCheckSlot()

	dockerfilePath := fixtureBuildFile(dir)
	snapshotDir := fixtureSnapshotDir(t, dir)
	outputFormat := lintFixtureOutputFormat(t, dir)
	args := []string{"lint"}
	if outputFormat == "" {
		outputFormat = "json"
		args = append(args, "--format", "json")
	}
	if !fileExists(filepath.Join(dir, ".tally.toml")) && !fileExists(filepath.Join(dir, "tally.toml")) {
		args = append(args, "--no-config")
	}
	args = append(args, dockerfilePath)
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = integrationCommandEnv()

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	exitCode := commandExitCode(t, err, stdoutBuf.String(), stderrBuf.String())
	if exitCode != 0 && exitCode != 1 {
		t.Fatalf("unexpected exit code %d\nstdout:\n%s\nstderr:\n%s", exitCode, stdoutBuf.String(), stderrBuf.String())
	}
	got := normalizeFixtureOutput(t, stdoutBuf.String())
	if outputFormat == "sarif" {
		got = normalizeBazelSARIFToolVersion(got)
	}
	if outputFormat == "json" || outputFormat == "sarif" {
		opts := []func(*snaps.Config){
			snaps.Dir(snapshotDir),
			snaps.Filename("result"),
			snaps.JSON(snaps.JSONConfig{Indent: "  ", SortKeys: true}),
		}
		if outputFormat == "sarif" {
			opts = append(opts, snaps.Ext(".sarif"))
		}
		integrationSnapshotConfig(opts...).MatchStandaloneJSON(t, got)
	} else {
		integrationSnapshotConfig(
			snaps.Dir(snapshotDir),
			snaps.Filename("result"),
			snaps.Ext(lintFixtureSnapshotExt(outputFormat)),
		).MatchStandaloneSnapshot(t, got)
	}

	if stderrBuf.Len() > 0 || fixtureSnapshotExists(t, dir, "stderr", ".txt") {
		gotStderr := normalizeFixtureOutput(t, stderrBuf.String())
		integrationSnapshotConfig(
			snaps.Dir(snapshotDir),
			snaps.Filename("stderr"),
			snaps.Ext(".txt"),
			snaps.Raw(),
		).MatchStandaloneSnapshot(t, gotStderr)
	}

	if filepath.Base(dir) == "slow-checks-fail-fast" && mockRegistry.HasRequest("library/slowfailfast") {
		t.Error("fail-fast should have prevented async check from fetching the slow image")
	}
}

func runFixFixture(t *testing.T, dir string) {
	t.Helper()
	releaseSlowCheckSlot := acquireSlowCheckFixtureSlot(t, dir)
	defer releaseSlowCheckSlot()

	buildFile := fixtureBuildFile(dir)
	snapshotDir := fixtureSnapshotDir(t, dir)
	input := readFixtureFile(t, buildFile)
	args := []string{"lint", "--format", "markdown", "--fix"}
	if fixtureHasContextFiles(t, dir) {
		args = append(args, "--context", ".")
	}
	if !fileExists(filepath.Join(dir, ".tally.toml")) && !fileExists(filepath.Join(dir, "tally.toml")) {
		args = append(args, "--no-config")
	}
	args = append(args, "-")
	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = dir
	cmd.Env = integrationCommandEnv()
	cmd.Stdin = strings.NewReader(input)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	exitCode := commandExitCode(t, err, stdoutBuf.String(), stderrBuf.String())
	wantExitCode := fixFixtureExpectedExitCode(t, dir, stderrBuf.String())
	if exitCode != wantExitCode {
		t.Fatalf("expected exit code %d, got %d\nstdout:\n%s\nstderr:\n%s", wantExitCode, exitCode, stdoutBuf.String(), stderrBuf.String())
	}

	gotFixed := normalizeFixtureOutput(t, stdoutBuf.String())
	integrationSnapshotConfig(
		snaps.Dir(snapshotDir),
		snaps.Filename("fixed"),
		snaps.Ext("."+filepath.Base(buildFile)),
		snaps.Raw(),
	).MatchStandaloneSnapshot(t, gotFixed)

	if stderrBuf.Len() == 0 && !fixtureSnapshotExists(t, dir, "report", ".md") {
		return
	}
	gotReport := normalizeFixtureOutput(t, stderrBuf.String())
	integrationSnapshotConfig(
		snaps.Dir(snapshotDir),
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
		output = strings.ReplaceAll(output, wd+string(os.PathSeparator), "")
		output = strings.ReplaceAll(output, filepath.ToSlash(wd)+"/", "")
	}
	return buildkitVersionRE.ReplaceAllString(output, "${1}0.0.0")
}

func normalizeBazelSARIFToolVersion(output string) string {
	return bazelDevToolVersionRE.ReplaceAllString(output, "${1}dev (buildkit v0.0.0)${2}")
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

func fixtureBuildFile(dir string) string {
	for _, name := range []string{"Dockerfile", "Containerfile"} {
		path := filepath.Join(dir, name)
		if fileExists(path) {
			return path
		}
	}
	return ""
}

func fixtureHasContextFiles(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read fixture dir %s: %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "Dockerfile" || name == "Containerfile" || name == ".tally.toml" || name == "tally.toml" {
			continue
		}
		if strings.Contains(name, ".snap.") {
			continue
		}
		return true
	}
	return false
}

type fixtureHarnessConfig struct {
	Output struct {
		Format    string `toml:"format"`
		FailLevel string `toml:"fail-level"`
	} `toml:"output"`
	SlowChecks struct {
		Mode string `toml:"mode"`
	} `toml:"slow-checks"`
}

func readFixtureHarnessConfig(t *testing.T, dir string) fixtureHarnessConfig {
	t.Helper()
	for _, name := range []string{".tally.toml", "tally.toml"} {
		path := filepath.Join(dir, name)
		if !fileExists(path) {
			continue
		}
		var cfg fixtureHarnessConfig
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if err := toml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		return cfg
	}
	return fixtureHarnessConfig{}
}

func lintFixtureOutputFormat(t *testing.T, dir string) string {
	t.Helper()
	return readFixtureHarnessConfig(t, dir).Output.Format
}

func acquireSlowCheckFixtureSlot(t *testing.T, dir string) func() {
	t.Helper()
	if readFixtureHarnessConfig(t, dir).SlowChecks.Mode != "on" {
		return func() {}
	}
	slowCheckFixtureSlots <- struct{}{}
	return func() {
		<-slowCheckFixtureSlots
	}
}

func fixFixtureExpectedExitCode(t *testing.T, dir, stderr string) int {
	t.Helper()
	if readFixtureHarnessConfig(t, dir).Output.FailLevel == "none" {
		return 0
	}
	if stderr == "" || strings.Contains(stderr, "**No issues found**") {
		return 0
	}
	return 1
}

func lintFixtureSnapshotExt(format string) string {
	switch format {
	case "github-actions":
		return ".txt"
	case "markdown":
		return ".md"
	default:
		return "." + format
	}
}

func fixtureSnapshotExists(t *testing.T, dir, filename, ext string) bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, filename+"_*.snap"+ext))
	if err != nil {
		t.Fatalf("glob fixture snapshots: %v", err)
	}
	return len(matches) > 0
}

func fixtureSnapshotDir(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolve fixture snapshot dir %s: %v", dir, err)
	}
	return abs
}
