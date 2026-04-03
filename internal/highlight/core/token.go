package core

import (
	"cmp"
	"slices"

	"github.com/wharflab/tally/internal/sourcemap"
)

type TokenType string

const (
	TokenKeyword   TokenType = "keyword"
	TokenComment   TokenType = "comment"
	TokenString    TokenType = "string"
	TokenNumber    TokenType = "number"
	TokenOperator  TokenType = "operator"
	TokenVariable  TokenType = "variable"
	TokenParameter TokenType = "parameter"
	TokenProperty  TokenType = "property"
	TokenFunction  TokenType = "function"
)

const (
	ModDeclaration uint32 = 1 << iota
	ModReadonly
	ModDocumentation
	ModDefaultLibrary
)

type Token struct {
	Line      int
	StartCol  int
	EndCol    int
	Type      TokenType
	Modifiers uint32
	Priority  int
}

func Normalize(sm *sourcemap.SourceMap, tokens []Token) []Token {
	if sm == nil || len(tokens) == 0 {
		return nil
	}

	byLine := make(map[int][]Token)
	for _, tok := range tokens {
		line := tok.Line
		if line < 0 || line >= sm.LineCount() {
			continue
		}

		lineLen := len([]rune(sm.Line(line)))
		if tok.StartCol < 0 {
			tok.StartCol = 0
		}
		if tok.EndCol > lineLen {
			tok.EndCol = lineLen
		}
		if tok.EndCol <= tok.StartCol {
			continue
		}
		byLine[line] = append(byLine[line], tok)
	}

	lines := make([]int, 0, len(byLine))
	for line := range byLine {
		lines = append(lines, line)
	}
	slices.Sort(lines)

	out := make([]Token, 0, len(tokens))
	for _, line := range lines {
		out = append(out, normalizeLine(line, byLine[line])...)
	}
	return out
}

func normalizeLine(line int, tokens []Token) []Token {
	if len(tokens) == 0 {
		return nil
	}

	bounds := make([]int, 0, len(tokens)*2)
	for _, tok := range tokens {
		bounds = append(bounds, tok.StartCol, tok.EndCol)
	}
	slices.Sort(bounds)

	uniq := bounds[:0]
	for _, bound := range bounds {
		if len(uniq) == 0 || uniq[len(uniq)-1] != bound {
			uniq = append(uniq, bound)
		}
	}
	if len(uniq) < 2 {
		return nil
	}

	out := make([]Token, 0, len(uniq)-1)
	for i := range len(uniq) - 1 {
		start, end := uniq[i], uniq[i+1]
		if end <= start {
			continue
		}

		best, ok := bestTokenForSegment(tokens, start, end)
		if !ok {
			continue
		}
		best.Line = line
		best.StartCol = start
		best.EndCol = end

		if len(out) > 0 {
			prev := &out[len(out)-1]
			if prev.Line == best.Line &&
				prev.EndCol == best.StartCol &&
				prev.Type == best.Type &&
				prev.Modifiers == best.Modifiers &&
				prev.Priority == best.Priority {
				prev.EndCol = best.EndCol
				continue
			}
		}
		out = append(out, best)
	}
	return out
}

func bestTokenForSegment(tokens []Token, start, end int) (Token, bool) {
	bestIdx := -1
	for i, tok := range tokens {
		if tok.StartCol > start || tok.EndCol < end {
			continue
		}
		if bestIdx == -1 || moreSpecific(tok, tokens[bestIdx]) {
			bestIdx = i
		}
	}
	if bestIdx == -1 {
		return Token{}, false
	}
	return tokens[bestIdx], true
}

func moreSpecific(a, b Token) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	if span := (a.EndCol - a.StartCol) - (b.EndCol - b.StartCol); span != 0 {
		return span < 0
	}
	if a.StartCol != b.StartCol {
		return a.StartCol > b.StartCol
	}
	if a.EndCol != b.EndCol {
		return a.EndCol < b.EndCol
	}
	if typ := cmp.Compare(string(a.Type), string(b.Type)); typ != 0 {
		return typ < 0
	}
	return a.Modifiers < b.Modifiers
}

func FilterRange(tokens []Token, startLine, startCol, endLine, endCol int) []Token {
	if len(tokens) == 0 {
		return nil
	}
	out := make([]Token, 0, len(tokens))
	for _, tok := range tokens {
		if tok.Line < startLine || tok.Line > endLine {
			continue
		}

		start := tok.StartCol
		end := tok.EndCol
		if tok.Line == startLine && start < startCol {
			start = startCol
		}
		if tok.Line == endLine && end > endCol {
			end = endCol
		}
		if end <= start {
			continue
		}

		tok.StartCol = start
		tok.EndCol = end
		out = append(out, tok)
	}
	return out
}

func ByLine(tokens []Token) map[int][]Token {
	if len(tokens) == 0 {
		return nil
	}
	out := make(map[int][]Token)
	for _, tok := range tokens {
		out[tok.Line] = append(out[tok.Line], tok)
	}
	return out
}
