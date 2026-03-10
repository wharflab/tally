package shell

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/wharflab/tally/internal/highlight/core"
	highlightpowershell "github.com/wharflab/tally/internal/highlight/powershell"
	myshell "github.com/wharflab/tally/internal/shell"

	shsyntax "mvdan.cc/sh/v3/syntax"
)

var (
	shellVarPattern    = regexp.MustCompile(`\$(\w+|\{[^}]+\})`)
	shellStringPattern = regexp.MustCompile(`"([^"\\]|\\.)*"|'([^'\\]|\\.)*'`)
)

func Tokenize(script string, variant myshell.Variant) []core.Token {
	if script == "" {
		return nil
	}
	if variant.IsPowerShell() {
		if tokens := highlightpowershell.Tokenize(script); tokens != nil {
			return tokens
		}
		return lexicalTokens(script)
	}
	if !variant.IsParseable() {
		return lexicalTokens(script)
	}

	parser := shsyntax.NewParser(
		shsyntax.Variant(langVariant(variant)),
		shsyntax.KeepComments(true),
	)
	file, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return lexicalTokens(script)
	}
	lines := strings.Split(script, "\n")

	var tokens []core.Token
	addNodeToken := func(pos, end shsyntax.Pos, typ core.TokenType, mods uint32) {
		tok, ok := tokenFromPositions(lines, pos, end, typ, mods)
		if !ok {
			return
		}
		tok.Priority = 30
		tokens = append(tokens, tok)
	}

	for _, c := range file.Last {
		addNodeToken(c.Pos(), c.End(), core.TokenComment, 0)
	}

	shsyntax.Walk(file, func(node shsyntax.Node) bool {
		switch n := node.(type) {
		case *shsyntax.Stmt:
			for _, c := range n.Comments {
				addNodeToken(c.Pos(), c.End(), core.TokenComment, 0)
			}
		case *shsyntax.CallExpr:
			if len(n.Args) > 0 && len(n.Args[0].Parts) > 0 {
				addWordPartToken(lines, n.Args[0].Parts[0], core.TokenFunction, 0, &tokens)
			}
		case *shsyntax.Assign:
			if n.Name != nil {
				addNodeToken(n.Name.Pos(), n.Name.End(), core.TokenVariable, core.ModDeclaration)
			}
		case *shsyntax.ParamExp:
			addNodeToken(n.Pos(), n.End(), core.TokenVariable, 0)
		case *shsyntax.SglQuoted:
			addNodeToken(n.Pos(), n.End(), core.TokenString, 0)
		case *shsyntax.DblQuoted:
			addNodeToken(n.Pos(), n.End(), core.TokenString, 0)
		case *shsyntax.Redirect:
			addNodeToken(n.OpPos, shsyntax.NewPos(n.OpPos.Offset()+1, n.OpPos.Line(), n.OpPos.Col()+1), core.TokenOperator, 0)
		}
		return true
	})

	return append(tokens, lexicalTokens(script)...)
}

func addWordPartToken(lines []string, part shsyntax.WordPart, typ core.TokenType, mods uint32, tokens *[]core.Token) {
	if part == nil {
		return
	}
	tok, ok := tokenFromPositions(lines, part.Pos(), part.End(), typ, mods)
	if !ok {
		return
	}
	tok.Priority = 30
	*tokens = append(*tokens, tok)
}

func tokenFromPositions(lines []string, pos, end shsyntax.Pos, typ core.TokenType, mods uint32) (core.Token, bool) {
	if !pos.IsValid() || !end.IsValid() || pos.Line() != end.Line() {
		return core.Token{}, false
	}
	line, ok := uintToInt(pos.Line())
	if !ok {
		return core.Token{}, false
	}
	start, ok := uintToInt(pos.Col())
	if !ok {
		return core.Token{}, false
	}
	finish, ok := uintToInt(end.Col())
	if !ok {
		return core.Token{}, false
	}
	line--
	lineContent, ok := lineContentAt(lines, line)
	if !ok {
		return core.Token{}, false
	}
	start = byteColumnToRuneColumn(lineContent, start-1)
	finish = byteColumnToRuneColumn(lineContent, finish-1)
	if finish <= start {
		finish = start + 1
	}
	return core.Token{
		Line:      line,
		StartCol:  start,
		EndCol:    finish,
		Type:      typ,
		Modifiers: mods,
		Priority:  30,
	}, true
}

func langVariant(variant myshell.Variant) shsyntax.LangVariant {
	switch variant {
	case myshell.VariantBash:
		return shsyntax.LangBash
	case myshell.VariantPOSIX:
		return shsyntax.LangPOSIX
	case myshell.VariantMksh:
		return shsyntax.LangMirBSDKorn
	case myshell.VariantZsh:
		return shsyntax.LangZsh
	case myshell.VariantPowerShell, myshell.VariantCmd, myshell.VariantUnknown:
		return shsyntax.LangBash
	}
	return shsyntax.LangBash
}

func uintToInt(v uint) (int, bool) {
	const maxInt = int(^uint(0) >> 1)
	if v > uint(maxInt) {
		return 0, false
	}
	return int(v), true
}

func lineContentAt(lines []string, line int) (string, bool) {
	if line < 0 {
		return "", false
	}
	if len(lines) == 0 {
		return "", true
	}
	if line >= len(lines) {
		return "", false
	}
	return lines[line], true
}

func byteColumnToRuneColumn(line string, byteCol int) int {
	byteCol = core.ClampByteIndex(line, byteCol)
	return utf8.RuneCountInString(line[:byteCol])
}

func lexicalTokens(script string) []core.Token {
	lines := strings.Split(script, "\n")
	tokens := make([]core.Token, 0, len(lines)*2)
	for lineNum, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			start := utf8.RuneCountInString(line[:len(line)-len(trimmed)])
			tokens = append(tokens, core.Token{
				Line:     lineNum,
				StartCol: start,
				EndCol:   len([]rune(line)),
				Type:     core.TokenComment,
				Priority: 25,
			})
			continue
		}

		for _, idx := range shellStringPattern.FindAllStringIndex(line, -1) {
			startCol, endCol := core.RuneColsForByteRange(line, idx[0], idx[1])
			tokens = append(tokens, core.Token{
				Line:     lineNum,
				StartCol: startCol,
				EndCol:   endCol,
				Type:     core.TokenString,
				Priority: 25,
			})
		}
		for _, idx := range shellVarPattern.FindAllStringIndex(line, -1) {
			startCol, endCol := core.RuneColsForByteRange(line, idx[0], idx[1])
			tokens = append(tokens, core.Token{
				Line:     lineNum,
				StartCol: startCol,
				EndCol:   endCol,
				Type:     core.TokenVariable,
				Priority: 26,
			})
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		first := fields[0]
		if first == "" || strings.HasPrefix(first, "#") {
			continue
		}
		if idx := strings.Index(line, first); idx >= 0 {
			startCol, endCol := core.RuneColsForByteRange(line, idx, idx+len(first))
			tokens = append(tokens, core.Token{
				Line:     lineNum,
				StartCol: startCol,
				EndCol:   endCol,
				Type:     core.TokenFunction,
				Priority: 24,
			})
		}
	}
	return tokens
}
