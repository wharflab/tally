//go:build cgo

package batch

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/highlight/tsutil"
	tsbatch "github.com/wharflab/tree-sitter-batch/bindings/go"
)

var batchNodeTokenTypes = map[string]core.TokenType{
	"comment":            core.TokenComment,
	"string":             core.TokenString,
	"variable_reference": core.TokenVariable,
	"for_variable":       core.TokenVariable,
	"integer":            core.TokenNumber,
	"comparison_op":      core.TokenOperator,
	"redirect_op":        core.TokenOperator,
	"command_option":     core.TokenParameter,
	"label":              core.TokenProperty, //nolint:customlint // tree-sitter node type, not Dockerfile instruction
}

var batchLanguage = newBatchLanguage()

func newBatchLanguage() *sitter.Language {
	ptr := tsbatch.Language()
	if ptr == nil {
		return nil
	}
	return sitter.NewLanguage(ptr)
}

// Tokenize returns parser-backed semantic tokens for cmd.exe/batch snippets.
func Tokenize(script string) []core.Token {
	return tsutil.Tokenize(script, batchLanguage, batchNodeTokenTypes)
}
