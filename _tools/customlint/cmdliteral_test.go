package customlint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCmdLiteral(t *testing.T) {
	t.Parallel()
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, cmdLiteralAnalyzer, "internal/rules/cmdliteral")
}
