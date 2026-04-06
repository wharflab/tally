//go:build cgo

package powershell

import (
	"strings"

	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/highlight/tsutil"
	tspowershell "github.com/wharflab/tree-sitter-powershell"
)

var powerShellHighlightQuery = tspowershell.HighlightsQuery

var powerShellCaptureSpecs = map[string]tsutil.CaptureSpec{
	"comment":   {Type: core.TokenComment},
	"delimiter": {Type: core.TokenOperator},
	"function":  {Type: core.TokenFunction},
	"keyword":   {Type: core.TokenKeyword},
	"number":    {Type: core.TokenNumber},
	"operator":  {Type: core.TokenOperator},
	"parameter": {Type: core.TokenParameter},
	"property":  {Type: core.TokenProperty},
	"string":    {Type: core.TokenString},
	"type":      {Type: core.TokenKeyword},
	"variable":  {Type: core.TokenVariable},
}

var powerShellLanguage = tspowershell.GetLanguage()

// Tokenize returns parser-backed semantic tokens for PowerShell snippets.
func Tokenize(script string) []core.Token {
	tokens := tsutil.TokenizeWithQuery(script, powerShellLanguage, powerShellHighlightQuery, powerShellCaptureSpecs)
	return filterPathFunctions(script, tokens)
}

// filterPathFunctions removes TokenFunction entries whose text looks like a
// filesystem path (e.g. C:\app\tool.exe, /usr/bin/curl) rather than a cmdlet.
func filterPathFunctions(script string, tokens []core.Token) []core.Token {
	if len(tokens) == 0 {
		return tokens
	}

	lines := strings.Split(script, "\n")
	out := tokens[:0]
	for _, tok := range tokens {
		if tok.Type == core.TokenFunction {
			if tok.Line >= 0 && tok.Line < len(lines) {
				runes := []rune(lines[tok.Line])
				if tok.StartCol >= 0 && tok.EndCol <= len(runes) {
					text := string(runes[tok.StartCol:tok.EndCol])
					if tsutil.CommandPathPattern.MatchString(text) {
						continue
					}
				}
			}
		}
		out = append(out, tok)
	}
	return out
}
