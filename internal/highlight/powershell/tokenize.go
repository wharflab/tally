//go:build cgo

package powershell

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/highlight/tsutil"
	tspowershell "github.com/wharflab/tally/internal/third_party/tree_sitter_powershell"
)

var powerShellNodeTokenTypes = map[string]core.TokenType{
	"comment":                         core.TokenComment,
	"string_literal":                  core.TokenString,
	"expandable_string_literal":       core.TokenString,
	"expandable_here_string_literal":  core.TokenString,
	"verbatim_string_characters":      core.TokenString,
	"verbatim_here_string_characters": core.TokenString,
	"variable":                        core.TokenVariable,
	"decimal_integer_literal":         core.TokenNumber,
	"hexadecimal_integer_literal":     core.TokenNumber,
	"real_literal":                    core.TokenNumber,
	"comparison_operator":             core.TokenOperator,
	"file_redirection_operator":       core.TokenOperator,
	"command_parameter":               core.TokenParameter,
	"member_name":                     core.TokenProperty,
}

var powerShellLanguage = newPowerShellLanguage()

func newPowerShellLanguage() *sitter.Language {
	ptr := tspowershell.Language()
	if ptr == nil {
		return nil
	}
	return sitter.NewLanguage(ptr)
}

// Tokenize returns parser-backed semantic tokens for PowerShell snippets.
// It keeps a conservative scope: comments, strings, variables, numbers,
// operators, parameters, property names, and command names that are not
// path-like native executables.
func Tokenize(script string) []core.Token {
	if script == "" {
		return nil
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if powerShellLanguage == nil {
		return nil
	}
	if err := parser.SetLanguage(powerShellLanguage); err != nil {
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
		if typ, ok := powerShellNodeTokenTypes[kind]; ok {
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
