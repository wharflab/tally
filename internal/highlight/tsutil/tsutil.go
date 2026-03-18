//go:build cgo

// Package tsutil provides shared tree-sitter-to-core.Token helpers used by
// dialect-specific tokenizers (powershell, batch).
package tsutil

import (
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/wharflab/tally/internal/highlight/core"
)

// CommandPathPattern matches command names that look like filesystem paths
// (e.g. C:\app\tool.exe, ./bin/tool, ~/script). These are typically excluded
// from TokenFunction output.
var CommandPathPattern = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\.{1,2}[\\/]|~[\\/]|[\\/])`)

// Tokenize parses script with the given tree-sitter language, walks the AST,
// and emits core.Token values for every named node whose Kind() appears in
// nodeTypes. Nodes with kind "command_name" that are not filesystem paths are
// emitted as TokenFunction. Returns nil if parsing fails.
func Tokenize(script string, lang *sitter.Language, nodeTypes map[string]core.TokenType) []core.Token {
	if script == "" || lang == nil {
		return nil
	}

	parser := sitter.NewParser()
	defer parser.Close()

	if err := parser.SetLanguage(lang); err != nil {
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

	Walk(tree.RootNode(), func(node *sitter.Node) {
		if node == nil || !node.IsNamed() {
			return
		}

		kind := node.Kind()
		if typ, ok := nodeTypes[kind]; ok {
			AppendNodeTokens(lines, node, typ, 30, 0, &tokens)
			return
		}

		if kind == "command_name" {
			text := strings.TrimSpace(node.Utf8Text(source))
			if text == "" || CommandPathPattern.MatchString(text) {
				return
			}
			AppendNodeTokens(lines, node, core.TokenFunction, 30, 0, &tokens)
		}
	})

	return tokens
}

// Walk visits every named node in the tree-sitter parse tree.
func Walk(node *sitter.Node, visit func(*sitter.Node)) {
	if node == nil {
		return
	}
	visit(node)
	childCount := node.NamedChildCount()
	for i := range childCount {
		Walk(node.NamedChild(i), visit)
	}
}

// AppendNodeTokens converts a tree-sitter node span into one core.Token per
// line and appends them to *tokens. Multi-line nodes are split into single-line
// tokens. Byte-based columns are converted to rune-based columns.
func AppendNodeTokens(lines []string, node *sitter.Node, typ core.TokenType, priority int, modifiers uint32, tokens *[]core.Token) {
	if node == nil {
		return
	}

	start := node.StartPosition()
	end := node.EndPosition()
	startLine := int(start.Row)
	endLine := int(end.Row)
	if startLine > endLine {
		return
	}

	for line := startLine; line <= endLine; line++ {
		lineContent, ok := LineContentAt(lines, line)
		if !ok {
			continue
		}

		startByte := 0
		endByte := len(lineContent)
		if line == startLine {
			startByte = int(start.Column)
		}
		if line == endLine {
			endByte = int(end.Column)
		}
		startCol, endCol := core.RuneColsForByteRange(lineContent, startByte, endByte)
		if endCol <= startCol {
			continue
		}

		*tokens = append(*tokens, core.Token{
			Line:      line,
			StartCol:  startCol,
			EndCol:    endCol,
			Type:      typ,
			Modifiers: modifiers,
			Priority:  priority,
		})
	}
}

// LineContentAt returns the content of a specific line from a pre-split slice.
func LineContentAt(lines []string, line int) (string, bool) {
	if line < 0 || line >= len(lines) {
		return "", false
	}
	return lines[line], true
}
