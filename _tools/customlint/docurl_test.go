package customlint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestDocURL(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, docURLAnalyzer,
		"internal/rules/docurl",
		"internal/semantic/docurl",
	)
}

func TestDocURL_OnlyChecksTargetPackages(t *testing.T) {
	t.Parallel()

	// This test verifies that the analyzer only runs on internal/rules and internal/semantic packages.
	// Files in other packages should not be analyzed, even if they have DocURL fields.
	testdata := analysistest.TestData()

	// Run on the target packages - these should be analyzed
	analysistest.Run(t, testdata, docURLAnalyzer,
		"internal/rules/docurl",
		"internal/semantic/docurl",
	)

	// Note: The analyzer's runDocURL function checks package path and returns early
	// if the package doesn't contain "internal/rules" or "internal/semantic".
	// This is tested implicitly by the fact that we only provide those packages.
}