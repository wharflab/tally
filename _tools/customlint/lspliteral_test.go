package customlint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestLSPLiteral(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, lspLiteralAnalyzer, "internal/lspserver")
}
