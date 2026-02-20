package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// MatchDockerfileSnapshot compares content against a standalone snapshot file,
// writing raw bytes without any formatting transformation.
//
// go-snaps' MatchStandaloneSnapshot passes content through pretty.Sprint
// (github.com/kr/pretty) whose tabwriter expands tab bytes (0x09) into
// spaces. All go-snaps helpers (snapshotPath, prettyDiff, etc.) are
// unexported so we cannot reuse them selectively. This thin helper
// preserves exact bytes so snapshot files match actual tally output.
//
// Tracking issue: https://github.com/gkampitakis/go-snaps/issues/153
//
// Follows go-snaps' naming convention for standalone snapshots:
//
//	__snapshots__/<TestName>_1.snap.Dockerfile
//
// Set UPDATE_SNAPS=true to create or update snapshot files.
func MatchDockerfileSnapshot(tb testing.TB, content string) {
	tb.Helper()

	_, callerFile, _, ok := runtime.Caller(1)
	if !ok {
		tb.Fatal("testutil.MatchDockerfileSnapshot: unable to determine caller")
	}

	name := strings.ReplaceAll(tb.Name(), "/", "_")
	snapFile := filepath.Join(filepath.Dir(callerFile), "__snapshots__", name+"_1.snap.Dockerfile")

	if os.Getenv("UPDATE_SNAPS") == "true" {
		if err := os.MkdirAll(filepath.Dir(snapFile), 0o750); err != nil {
			tb.Fatalf("mkdir snapshot dir: %v", err)
		}
		if err := os.WriteFile(snapFile, []byte(content), 0o644); err != nil { //nolint:gosec // test-only snapshot
			tb.Fatalf("write snapshot: %v", err)
		}
		return
	}

	prev, err := os.ReadFile(snapFile)
	if err != nil {
		tb.Fatalf("snapshot not found: %s\nRun with UPDATE_SNAPS=true to create", snapFile)
	}
	if string(prev) != content {
		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(string(prev), content, true)
		diffs = dmp.DiffCleanupSemanticLossless(diffs)
		patches := dmp.PatchMake(string(prev), diffs)
		tb.Errorf("snapshot mismatch: %s\n%s", snapFile, dmp.PatchToText(patches))
	}
}
