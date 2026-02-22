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
