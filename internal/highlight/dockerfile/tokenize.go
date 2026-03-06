package dockerfile

import (
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/sourcemap"
)

var (
	flagPattern        = regexp.MustCompile(`--[a-zA-Z][a-zA-Z0-9-]*`)
	varPattern         = regexp.MustCompile(`\$(\w+|\{[^}]+\})`)
	stringPattern      = regexp.MustCompile(`"([^"\\]|\\.)*"|'([^'\\]|\\.)*'`)
	numberPattern      = regexp.MustCompile(`\b\d+\b`)
	heredocPattern     = regexp.MustCompile(`<<-?\s*([A-Za-z0-9_'"-]+)`)
	fromAliasPattern   = regexp.MustCompile(`(?i)\bAS\b\s+([A-Za-z0-9._-]+)`)
	instructionPattern = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9_-]*)\b`)
)

func Tokenize(sm *sourcemap.SourceMap, root *parser.Node, escapeToken rune) []core.Token {
	if sm == nil {
		return nil
	}

	excludedLines := heredocBodyLines(sm, root, escapeToken)
	tokens := commentTokens(sm, excludedLines)
	if root == nil {
		return append(tokens, fallbackLineTokens(sm, excludedLines)...)
	}

	for _, node := range root.Children {
		if node == nil || node.StartLine <= 0 {
			continue
		}
		tokens = append(tokens, tokenizeNode(sm, node, escapeToken, excludedLines)...)
	}
	return tokens
}

func tokenizeNode(
	sm *sourcemap.SourceMap,
	node *parser.Node,
	escapeToken rune,
	excludedLines map[int]bool,
) []core.Token {
	startLine := node.StartLine
	endLine := sm.ResolveEndLineWithEscape(node.EndLine, escapeToken)
	endLine = max(endLine, startLine)

	tokens := make([]core.Token, 0, 12)
	for line := startLine; line <= endLine; line++ {
		lineIdx := line - 1
		if excludedLines[lineIdx] {
			continue
		}

		text := sm.Line(lineIdx)
		if line == startLine {
			if tok, ok := instructionKeywordToken(text, lineIdx); ok {
				tokens = append(tokens, tok)
			}
			tokens = append(tokens, fromAliasTokens(text, lineIdx)...)
		}

		tokens = append(tokens, flagTokens(text, lineIdx)...)
		tokens = append(tokens, quotedTokens(text, lineIdx)...)
		tokens = append(tokens, variableTokens(text, lineIdx)...)
		tokens = append(tokens, numberTokens(text, lineIdx)...)
		tokens = append(tokens, heredocTokens(text, lineIdx)...)
	}
	return tokens
}

func instructionKeywordToken(line string, lineNum int) (core.Token, bool) {
	m := instructionPattern.FindStringSubmatchIndex(line)
	if m == nil {
		return core.Token{}, false
	}
	return core.Token{
		Line:     lineNum,
		StartCol: m[2],
		EndCol:   m[3],
		Type:     core.TokenKeyword,
		Priority: 20,
	}, true
}

func fromAliasTokens(line string, lineNum int) []core.Token {
	matches := fromAliasPattern.FindAllStringSubmatchIndex(line, -1)
	out := make([]core.Token, 0, len(matches)*2)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		matchStart, matchEnd := m[0], m[1]
		aliasStart, aliasEnd := m[2], m[3]
		matchText := strings.ToUpper(line[matchStart:matchEnd])
		asOffset := strings.Index(matchText, "AS")
		if asOffset < 0 {
			continue
		}
		out = append(out,
			core.Token{
				Line:     lineNum,
				StartCol: matchStart + asOffset,
				EndCol:   matchStart + asOffset + 2,
				Type:     core.TokenKeyword,
				Priority: 21,
			},
			core.Token{
				Line:      lineNum,
				StartCol:  aliasStart,
				EndCol:    aliasEnd,
				Type:      core.TokenVariable,
				Modifiers: core.ModDeclaration,
				Priority:  22,
			},
		)
	}
	return out
}

func flagTokens(line string, lineNum int) []core.Token {
	matches := flagPattern.FindAllStringIndex(line, -1)
	out := make([]core.Token, 0, len(matches)*3)
	for _, idx := range matches {
		start, end := idx[0], idx[1]
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: start,
			EndCol:   end,
			Type:     core.TokenParameter,
			Priority: 22,
		})

		if end >= len(line) || line[end] != '=' {
			continue
		}
		valueStart := end + 1
		valueEnd := valueStart
		for valueEnd < len(line) && line[valueEnd] != ' ' && line[valueEnd] != '\t' {
			valueEnd++
		}
		value := line[valueStart:valueEnd]
		if strings.Contains(value, "=") && strings.Contains(value, ",") {
			out = append(out, kvValueTokens(value, lineNum, valueStart)...)
			continue
		}
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: valueStart,
			EndCol:   valueEnd,
			Type:     core.TokenString,
			Priority: 21,
		})
	}
	return out
}

func kvValueTokens(value string, lineNum, baseCol int) []core.Token {
	parts := strings.Split(value, ",")
	out := make([]core.Token, 0, len(parts)*2)
	offset := 0
	for _, part := range parts {
		if part == "" {
			offset++
			continue
		}
		if eq := strings.Index(part, "="); eq >= 0 {
			out = append(out,
				core.Token{
					Line:     lineNum,
					StartCol: baseCol + offset,
					EndCol:   baseCol + offset + eq,
					Type:     core.TokenProperty,
					Priority: 22,
				},
				core.Token{
					Line:     lineNum,
					StartCol: baseCol + offset + eq + 1,
					EndCol:   baseCol + offset + len(part),
					Type:     core.TokenString,
					Priority: 21,
				},
			)
		} else {
			out = append(out, core.Token{
				Line:     lineNum,
				StartCol: baseCol + offset,
				EndCol:   baseCol + offset + len(part),
				Type:     core.TokenString,
				Priority: 21,
			})
		}
		offset += len(part) + 1
	}
	return out
}

func quotedTokens(line string, lineNum int) []core.Token {
	matches := stringPattern.FindAllStringIndex(line, -1)
	out := make([]core.Token, 0, len(matches))
	for _, idx := range matches {
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: idx[0],
			EndCol:   idx[1],
			Type:     core.TokenString,
			Priority: 18,
		})
	}
	return out
}

func variableTokens(line string, lineNum int) []core.Token {
	matches := varPattern.FindAllStringIndex(line, -1)
	out := make([]core.Token, 0, len(matches))
	for _, idx := range matches {
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: idx[0],
			EndCol:   idx[1],
			Type:     core.TokenVariable,
			Priority: 23,
		})
	}
	return out
}

func numberTokens(line string, lineNum int) []core.Token {
	matches := numberPattern.FindAllStringIndex(line, -1)
	out := make([]core.Token, 0, len(matches))
	for _, idx := range matches {
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: idx[0],
			EndCol:   idx[1],
			Type:     core.TokenNumber,
			Priority: 17,
		})
	}
	return out
}

func heredocTokens(line string, lineNum int) []core.Token {
	matches := heredocPattern.FindAllStringSubmatchIndex(line, -1)
	out := make([]core.Token, 0, len(matches)*2)
	for _, m := range matches {
		nameStart, nameEnd := m[2], m[3]
		out = append(out,
			core.Token{
				Line:     lineNum,
				StartCol: m[0],
				EndCol:   nameStart,
				Type:     core.TokenOperator,
				Priority: 24,
			},
			core.Token{
				Line:     lineNum,
				StartCol: nameStart,
				EndCol:   nameEnd,
				Type:     core.TokenString,
				Priority: 24,
			},
		)
	}
	return out
}

func commentTokens(sm *sourcemap.SourceMap, excludedLines map[int]bool) []core.Token {
	out := make([]core.Token, 0)
	for i, line := range sm.Lines() {
		if excludedLines[i] {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		start := len(line) - len(trimmed)
		out = append(out, core.Token{
			Line:      i,
			StartCol:  start,
			EndCol:    len([]rune(line)),
			Type:      core.TokenComment,
			Modifiers: core.ModDocumentation,
			Priority:  40,
		})
	}
	return out
}

func fallbackLineTokens(sm *sourcemap.SourceMap, excludedLines map[int]bool) []core.Token {
	out := make([]core.Token, 0, sm.LineCount()*2)
	for i, line := range sm.Lines() {
		if excludedLines[i] {
			continue
		}
		if tok, ok := instructionKeywordToken(line, i); ok {
			out = append(out, tok)
		}
		out = append(out, flagTokens(line, i)...)
		out = append(out, quotedTokens(line, i)...)
		out = append(out, variableTokens(line, i)...)
		out = append(out, heredocTokens(line, i)...)
	}
	return out
}

func heredocBodyLines(sm *sourcemap.SourceMap, root *parser.Node, escapeToken rune) map[int]bool {
	if sm == nil || root == nil {
		return nil
	}
	out := make(map[int]bool)
	for _, node := range root.Children {
		if node == nil || len(node.Heredocs) == 0 || node.StartLine <= 0 {
			continue
		}
		end := sm.ResolveEndLineWithEscape(node.EndLine, escapeToken)
		for line := node.StartLine; line <= end; line++ {
			if line == node.StartLine || line == end {
				continue
			}
			out[line-1] = true
		}
	}
	return out
}
