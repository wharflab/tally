//go:build cgo

package batch

import (
	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/highlight/tsutil"
	tsbatch "github.com/wharflab/tree-sitter-batch"
)

var batchHighlightQuery = tsbatch.HighlightsQuery

var batchCaptureSpecs = map[string]tsutil.CaptureSpec{
	"comment":          {Type: core.TokenComment},
	"constant":         {Type: core.TokenParameter},
	"function":         {Type: core.TokenFunction},
	"keyword":          {Type: core.TokenKeyword},
	"label":            {Type: core.TokenProperty}, //nolint:customlint // tree-sitter capture, not Dockerfile instruction
	"number":           {Type: core.TokenNumber},
	"operator":         {Type: core.TokenOperator},
	"string":           {Type: core.TokenString},
	"string.special":   {Type: core.TokenString},
	"variable":         {Type: core.TokenVariable},
	"variable.builtin": {Type: core.TokenVariable, Modifiers: core.ModDefaultLibrary, Priority: 31},
}

var batchLanguage = tsbatch.GetLanguage()

// Tokenize returns parser-backed semantic tokens for cmd.exe/batch snippets.
func Tokenize(script string) []core.Token {
	return tsutil.TokenizeWithQuery(script, batchLanguage, batchHighlightQuery, batchCaptureSpecs)
}
