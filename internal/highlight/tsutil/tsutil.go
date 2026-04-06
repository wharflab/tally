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

// CaptureSpec describes how a tree-sitter query capture should map to a token.
type CaptureSpec struct {
	Type      core.TokenType
	Modifiers uint32
	Priority  int
}

// CommandPathPattern matches command names that look like filesystem paths
// (e.g. C:\app\tool.exe, ./bin/tool, ~/script). These are typically excluded
// from TokenFunction output.
var CommandPathPattern = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\.{1,2}[\\/]|~[\\/]|[\\/])`)

// TokenizeWithQuery parses script with the given tree-sitter language, applies
// the supplied query, and maps captures to core.Token values. Capture names are
// matched exactly first, then by their base name before the first dot, so a
// query capture like @variable.builtin can reuse a generic "variable" mapping.
func TokenizeWithQuery(
	script string,
	lang *sitter.Language,
	querySource string,
	captureSpecs map[string]CaptureSpec,
) []core.Token {
	if script == "" || lang == nil || strings.TrimSpace(querySource) == "" {
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

	query, err := sitter.NewQuery(lang, querySource)
	if err != nil {
		return nil
	}
	defer query.Close()

	lines := strings.Split(script, "\n")
	captureNames := query.CaptureNames()
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	matches := cursor.Matches(query, tree.RootNode(), source)
	tokens := make([]core.Token, 0, 16)
	for match := matches.Next(); match != nil; match = matches.Next() {
		for _, capture := range match.Captures {
			name, ok := captureName(captureNames, capture.Index)
			if !ok {
				continue
			}

			spec, ok := captureSpecForName(name, captureSpecs)
			if !ok {
				continue
			}

			node := capture.Node
			priority := spec.Priority
			if priority == 0 {
				priority = 30
			}
			AppendNodeTokens(lines, &node, spec.Type, priority, spec.Modifiers, &tokens)
		}
	}

	return tokens
}

func captureName(names []string, index uint32) (string, bool) {
	if int(index) >= len(names) {
		return "", false
	}
	return names[index], true
}

func captureSpecForName(name string, specs map[string]CaptureSpec) (CaptureSpec, bool) {
	if spec, ok := specs[name]; ok {
		return spec, true
	}

	if before, _, ok := strings.Cut(name, "."); ok {
		if spec, ok := specs[before]; ok {
			return spec, true
		}
	}

	return CaptureSpec{}, false
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
