package facts

import (
	"testing"

	"github.com/wharflab/tally/internal/semantic"
)

func TestStageFactsObservablePathHelpers_WindowsCaseInsensitive(t *testing.T) {
	t.Parallel()

	stage := &StageFacts{
		BaseImageOS: semantic.BaseImageOSWindows,
		ObservableFiles: []*ObservableFile{
			{Path: `C:\APP\.YARN\RELEASES\YARN-4.2.2.CJS`},
			{Path: `C:\CURL\_CURLRC`},
		},
	}

	if !stage.HasObservablePathSuffix("/_curlrc") {
		t.Fatal("HasObservablePathSuffix(/_curlrc) = false, want true")
	}

	var gotBase string
	var gotHasSegment bool
	stage.ScanObservableFiles(func(_ *ObservableFile, path ObservablePathView) bool {
		if path.HasSegment(".yarn") {
			gotBase = path.Base()
			gotHasSegment = true
			return false
		}
		return true
	})

	if !gotHasSegment {
		t.Fatal("ScanObservableFiles did not match .yarn segment")
	}
	if gotBase != "yarn-4.2.2.cjs" {
		t.Fatalf("Base() = %q, want %q", gotBase, "yarn-4.2.2.cjs")
	}
}

func TestStageFactsObservablePathHelpers_LinuxCaseSensitive(t *testing.T) {
	t.Parallel()

	stage := &StageFacts{
		BaseImageOS: semantic.BaseImageOSLinux,
		ObservableFiles: []*ObservableFile{
			{Path: "/CURL/_CURLRC"},
			{Path: "/app/.YARN/releases/yarn-4.2.2.cjs"},
		},
	}

	if stage.HasObservablePathSuffix("/_curlrc") {
		t.Fatal("HasObservablePathSuffix(/_curlrc) = true, want false")
	}

	var matched bool
	stage.ScanObservableFiles(func(_ *ObservableFile, path ObservablePathView) bool {
		if path.HasSegment(".yarn") {
			matched = true
			return false
		}
		return true
	})

	if matched {
		t.Fatal("ScanObservableFiles matched .yarn on Linux case-sensitive path, want false")
	}
}
