package dockerfile

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/sourcemap"
)

var (
	flagPattern        = regexp.MustCompile(`--[a-zA-Z][a-zA-Z0-9-]*`)
	varPattern         = regexp.MustCompile(`\$(\w+|\{[^}]+\})`)
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
		return append(tokens, fallbackLineTokens(sm, excludedLines, escapeToken)...)
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
				if strings.EqualFold(tokenText(text, tok), command.From) {
					tokens = append(tokens, fromAliasTokens(text, lineIdx)...)
				}
			}
		}

		tokens = append(tokens, flagTokens(text, lineIdx)...)
		quoted := quotedTokens(text, lineIdx, escapeToken)
		tokens = append(tokens, quoted...)
		tokens = append(tokens, windowsPathTokens(text, lineIdx, quoted)...)
		tokens = append(tokens, variableTokens(text, lineIdx)...)
		tokens = append(tokens, numberTokens(text, lineIdx)...)
		tokens = append(tokens, heredocTokens(text, lineIdx)...)
	}
	return tokens
}

func tokenText(line string, tok core.Token) string {
	runes := []rune(line)
	if tok.StartCol < 0 || tok.EndCol < tok.StartCol || tok.EndCol > len(runes) {
		return ""
	}
	return string(runes[tok.StartCol:tok.EndCol])
}

func instructionKeywordToken(line string, lineNum int) (core.Token, bool) {
	m := instructionPattern.FindStringSubmatchIndex(line)
	if m == nil {
		return core.Token{}, false
	}
	startCol, endCol := core.RuneColsForByteRange(line, m[2], m[3])
	return core.Token{
		Line:     lineNum,
		StartCol: startCol,
		EndCol:   endCol,
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
		asStartCol, asEndCol := core.RuneColsForByteRange(line, matchStart+asOffset, matchStart+asOffset+2)
		aliasStartCol, aliasEndCol := core.RuneColsForByteRange(line, aliasStart, aliasEnd)
		out = append(out,
			core.Token{
				Line:     lineNum,
				StartCol: asStartCol,
				EndCol:   asEndCol,
				Type:     core.TokenKeyword,
				Priority: 21,
			},
			core.Token{
				Line:      lineNum,
				StartCol:  aliasStartCol,
				EndCol:    aliasEndCol,
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
		startByte, endByte := idx[0], idx[1]
		startCol, endCol := core.RuneColsForByteRange(line, startByte, endByte)
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: startCol,
			EndCol:   endCol,
			Type:     core.TokenParameter,
			Priority: 22,
		})

		if endByte >= len(line) || line[endByte] != '=' {
			continue
		}
		valueStartByte := endByte + 1
		valueEndByte := valueStartByte
		for valueEndByte < len(line) && line[valueEndByte] != ' ' && line[valueEndByte] != '\t' {
			valueEndByte++
		}
		value := line[valueStartByte:valueEndByte]
		if strings.Contains(value, "=") && strings.Contains(value, ",") {
			out = append(out, kvValueTokens(line, value, lineNum, valueStartByte)...)
			continue
		}
		valueStartCol, valueEndCol := core.RuneColsForByteRange(line, valueStartByte, valueEndByte)
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: valueStartCol,
			EndCol:   valueEndCol,
			Type:     core.TokenString,
			Priority: 21,
		})
	}
	return out
}

func kvValueTokens(line, value string, lineNum, baseByte int) []core.Token {
	parts := strings.Split(value, ",")
	out := make([]core.Token, 0, len(parts)*2)
	offsetBytes := 0
	for _, part := range parts {
		if part == "" {
			offsetBytes++
			continue
		}
		if eq := strings.Index(part, "="); eq >= 0 {
			propStartCol, propEndCol := core.RuneColsForByteRange(line, baseByte+offsetBytes, baseByte+offsetBytes+eq)
			valStartCol, valEndCol := core.RuneColsForByteRange(
				line,
				baseByte+offsetBytes+eq+1,
				baseByte+offsetBytes+len(part),
			)
			out = append(out,
				core.Token{
					Line:     lineNum,
					StartCol: propStartCol,
					EndCol:   propEndCol,
					Type:     core.TokenProperty,
					Priority: 22,
				},
				core.Token{
					Line:     lineNum,
					StartCol: valStartCol,
					EndCol:   valEndCol,
					Type:     core.TokenString,
					Priority: 21,
				},
			)
		} else {
			startCol, endCol := core.RuneColsForByteRange(line, baseByte+offsetBytes, baseByte+offsetBytes+len(part))
			out = append(out, core.Token{
				Line:     lineNum,
				StartCol: startCol,
				EndCol:   endCol,
				Type:     core.TokenString,
				Priority: 21,
			})
		}
		offsetBytes += len(part) + 1
	}
	return out
}

func quotedTokens(line string, lineNum int, escapeToken rune) []core.Token {
	ranges := quotedRanges(line, escapeToken)
	out := make([]core.Token, 0, len(ranges))
	for _, rng := range ranges {
		startCol, endCol := core.RuneColsForByteRange(line, rng[0], rng[1])
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: startCol,
			EndCol:   endCol,
			Type:     core.TokenString,
			Priority: 18,
		})
	}
	return out
}

func quotedRanges(line string, escapeToken rune) [][2]int {
	out := make([][2]int, 0, 2)
	for i := 0; i < len(line); i++ {
		quote := line[i]
		if quote != '"' && quote != '\'' {
			continue
		}

		start := i
		i++
		for i < len(line) {
			if line[i] == quote && !isEscapedBy(line, i, byte(escapeToken)) {
				out = append(out, [2]int{start, i + 1})
				break
			}
			i++
		}
	}
	return out
}

func isEscapedBy(line string, idx int, escape byte) bool {
	if escape == 0 || idx <= 0 {
		return false
	}
	count := 0
	for i := idx - 1; i >= 0 && line[i] == escape; i-- {
		count++
	}
	return count%2 == 1
}

func variableTokens(line string, lineNum int) []core.Token {
	matches := varPattern.FindAllStringIndex(line, -1)
	out := make([]core.Token, 0, len(matches))
	for _, idx := range matches {
		startCol, endCol := core.RuneColsForByteRange(line, idx[0], idx[1])
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: startCol,
			EndCol:   endCol,
			Type:     core.TokenVariable,
			Priority: 23,
		})
	}
	return out
}

func windowsPathTokens(line string, lineNum int, quoted []core.Token) []core.Token {
	out := make([]core.Token, 0, 2)
	for i := 0; i < len(line); i++ {
		start, ok := windowsPathStart(line, i)
		if !ok || start != i {
			continue
		}
		if inQuotedToken(quoted, line, start) {
			continue
		}

		end := windowsPathEnd(line, start)
		if end <= start {
			continue
		}
		startCol, endCol := core.RuneColsForByteRange(line, start, end)
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: startCol,
			EndCol:   endCol,
			Type:     core.TokenString,
			Priority: 28,
		})
		i = end - 1
	}
	return out
}

func windowsPathStart(line string, idx int) (int, bool) {
	if idx < 0 || idx >= len(line) {
		return 0, false
	}
	if idx > 0 && !isWindowsPathBoundary(line[idx-1]) {
		return 0, false
	}

	if idx+2 < len(line) && isAlpha(line[idx]) && line[idx+1] == ':' && line[idx+2] == '\\' {
		return idx, true
	}
	if idx+1 < len(line) && line[idx] == '.' && line[idx+1] == '\\' {
		return idx, true
	}
	if idx+2 < len(line) && line[idx] == '.' && line[idx+1] == '.' && line[idx+2] == '\\' {
		return idx, true
	}
	return 0, false
}

func windowsPathEnd(line string, start int) int {
	end := start
	for end < len(line) {
		switch line[end] {
		case ' ', '\t', ',', ';', '"', '\'', '[', ']', '(', ')', '{', '}':
			return end
		}
		end++
	}
	return end
}

func inQuotedToken(tokens []core.Token, line string, startByte int) bool {
	if len(tokens) == 0 {
		return false
	}
	startCol, _ := core.RuneColsForByteRange(line, startByte, startByte)
	for _, tok := range tokens {
		if tok.Type == core.TokenString && tok.StartCol < startCol && tok.EndCol > startCol {
			return true
		}
	}
	return false
}

func isWindowsPathBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '=', ',', ';', '"', '\'', '[', ']', '(', ')', '{', '}':
		return true
	}
	return false
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func numberTokens(line string, lineNum int) []core.Token {
	matches := numberPattern.FindAllStringIndex(line, -1)
	out := make([]core.Token, 0, len(matches))
	for _, idx := range matches {
		startCol, endCol := core.RuneColsForByteRange(line, idx[0], idx[1])
		out = append(out, core.Token{
			Line:     lineNum,
			StartCol: startCol,
			EndCol:   endCol,
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
		opStartCol, opEndCol := core.RuneColsForByteRange(line, m[0], nameStart)
		nameStartCol, nameEndCol := core.RuneColsForByteRange(line, nameStart, nameEnd)
		out = append(out,
			core.Token{
				Line:     lineNum,
				StartCol: opStartCol,
				EndCol:   opEndCol,
				Type:     core.TokenOperator,
				Priority: 24,
			},
			core.Token{
				Line:     lineNum,
				StartCol: nameStartCol,
				EndCol:   nameEndCol,
				Type:     core.TokenString,
				Priority: 24,
			},
		)
	}
	return out
}

func commentTokens(sm *sourcemap.SourceMap, excludedLines map[int]bool) []core.Token {
	directiveComments := make(map[int]sourcemap.Comment)
	for _, comment := range sm.Comments() {
		if comment.IsDirective {
			directiveComments[comment.Line] = comment
		}
	}

	out := make([]core.Token, 0)
	for i, line := range sm.Lines() {
		if excludedLines[i] {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		if _, ok := directiveComments[i]; ok {
			if directiveTokens := directiveCommentTokens(line, i, trimmed); len(directiveTokens) > 0 {
				out = append(out, directiveTokens...)
				continue
			}
		}
		start := utf8.RuneCountInString(line[:len(line)-len(trimmed)])
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

func directiveCommentTokens(line string, lineNum int, text string) []core.Token {
	indentBytes := len(line) - len(strings.TrimLeft(line, " \t"))
	lexed := directive.LexComment(text)
	if len(lexed) == 0 {
		return nil
	}

	out := make([]core.Token, 0, len(lexed))
	for _, tok := range lexed {
		typ, priority := directiveSemanticType(tok.Kind)
		out = append(out, byteRangeToken(line, lineNum, indentBytes+tok.StartByte, indentBytes+tok.EndByte, typ, priority))
	}
	return out
}

func byteRangeToken(
	line string,
	lineNum int,
	startByte int,
	endByte int,
	typ core.TokenType,
	priority int,
) core.Token {
	startCol, endCol := core.RuneColsForByteRange(line, startByte, endByte)
	return core.Token{
		Line:     lineNum,
		StartCol: startCol,
		EndCol:   endCol,
		Type:     typ,
		Priority: priority,
	}
}

func directiveSemanticType(kind directive.CommentTokenKind) (core.TokenType, int) {
	switch kind {
	case directive.CommentTokenKeyword:
		return core.TokenKeyword, 34
	case directive.CommentTokenOperator:
		return core.TokenOperator, 33
	case directive.CommentTokenRule:
		return core.TokenProperty, 32
	case directive.CommentTokenValue:
		return core.TokenString, 31
	default:
		return core.TokenString, 31
	}
}

func fallbackLineTokens(sm *sourcemap.SourceMap, excludedLines map[int]bool, escapeToken rune) []core.Token {
	out := make([]core.Token, 0, sm.LineCount()*2)
	for i, line := range sm.Lines() {
		if excludedLines[i] {
			continue
		}
		if tok, ok := instructionKeywordToken(line, i); ok {
			out = append(out, tok)
		}
		out = append(out, flagTokens(line, i)...)
		quoted := quotedTokens(line, i, escapeToken)
		out = append(out, quoted...)
		out = append(out, windowsPathTokens(line, i, quoted)...)
		out = append(out, variableTokens(line, i)...)
		out = append(out, numberTokens(line, i)...)
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
