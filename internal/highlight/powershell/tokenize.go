//go:build cgo

package powershell

import (
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
func Tokenize(script string) []core.Token {
	return tsutil.Tokenize(script, powerShellLanguage, powerShellNodeTokenTypes)
}
