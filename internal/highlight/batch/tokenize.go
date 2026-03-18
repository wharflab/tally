//go:build cgo

package batch

import (
	"strings"

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
// It covers comments, strings, variables, numbers, operators, command options,
// labels, and command names that are not path-like native executables.
func Tokenize(script string) []core.Token {
	if script == "" {
		return nil
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if batchLanguage == nil {
		return nil
	}
	if err := parser.SetLanguage(batchLanguage); err != nil {
		return nil
	}

	source := []byte(script)
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	lines := strings.Split(script, "\n")
	tokens := make([]core.Token, 0, 16)

	tsutil.Walk(tree.RootNode(), func(node *sitter.Node) {
		if node == nil || !node.IsNamed() {
			return
		}

		kind := node.Kind()
		if typ, ok := batchNodeTokenTypes[kind]; ok {
			tsutil.AppendNodeTokens(lines, node, typ, 30, 0, &tokens)
			return
		}

		if kind == "command_name" {
			text := strings.TrimSpace(node.Utf8Text(source))
			if text == "" || tsutil.CommandPathPattern.MatchString(text) {
				return
			}
			tsutil.AppendNodeTokens(lines, node, core.TokenFunction, 30, 0, &tokens)
		}
	})

	return tokens
}
