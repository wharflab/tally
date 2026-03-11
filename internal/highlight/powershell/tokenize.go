package powershell

import (
	"regexp"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"

	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/powershellast"
	"github.com/wharflab/tally/internal/powershellast/queries"
)

var commandPathPattern = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\.{1,2}[\\/]|~[\\/]|[\\/])`)

const semanticPriority = 30

// Tokenize returns parser-backed semantic tokens for PowerShell snippets.
func Tokenize(script string) []core.Token {
	if script == "" {
		return nil
	}

	lang := powershellast.Language()
	query := powershellast.SemanticQuery()
	tree, source := powershellast.Parse(script)
	if tree == nil || lang == nil || query == nil {
		return nil
	}
	defer tree.Release()

	lines := strings.Split(script, "\n")
	tokens := make([]core.Token, 0, 16)
	cursor := query.Exec(tree.RootNode(), lang, source)

	for {
		match, ok := cursor.Next()
		if !ok {
			break
		}
		appendSemanticMatchTokens(lines, match, &tokens)
	}

	appendCommandTokens(lines, tree.RootNode(), lang, source, &tokens)

	return tokens
}

func appendSemanticMatchTokens(lines []string, match gotreesitter.QueryMatch, tokens *[]core.Token) {
	switch match.PatternIndex {
	case 0:
		appendNodeTokens(lines, queries.MatchPattern0(match).Comment, core.TokenComment, false, tokens)
	case 1:
		appendNodeTokens(lines, queries.MatchPattern1(match).Variable, core.TokenVariable, false, tokens)
	case 2:
		appendNodeTokens(lines, queries.MatchPattern2(match).Number, core.TokenNumber, false, tokens)
	case 3:
		appendNodeTokens(lines, queries.MatchPattern3(match).Number, core.TokenNumber, false, tokens)
	case 4:
		appendNodeTokens(lines, queries.MatchPattern4(match).Number, core.TokenNumber, false, tokens)
	case 5:
		appendNodeTokens(lines, queries.MatchPattern5(match).Operator, core.TokenOperator, false, tokens)
	case 6:
		appendNodeTokens(lines, queries.MatchPattern6(match).Operator, core.TokenOperator, false, tokens)
	case 7:
		appendNodeTokens(lines, queries.MatchPattern7(match).Parameter, core.TokenParameter, false, tokens)
	case 8:
		appendNodeTokens(lines, queries.MatchPattern8(match).Property, core.TokenProperty, false, tokens)
	case 9:
		appendNodeTokens(lines, queries.MatchPattern9(match).String, core.TokenString, true, tokens)
	case 10:
		appendNodeTokens(lines, queries.MatchPattern10(match).String, core.TokenString, true, tokens)
	case 11:
		appendNodeTokens(lines, queries.MatchPattern11(match).String, core.TokenString, true, tokens)
	case 12:
		appendNodeTokens(lines, queries.MatchPattern12(match).String, core.TokenString, true, tokens)
	case 13:
		appendNodeTokens(lines, queries.MatchPattern13(match).String, core.TokenString, true, tokens)
	}
}

func appendCommandTokens(
	lines []string,
	root *gotreesitter.Node,
	lang *gotreesitter.Language,
	source []byte,
	tokens *[]core.Token,
) {
	commandQuery := powershellast.CommandsQuery()
	if commandQuery == nil {
		return
	}

	commandCursor := commandQuery.Exec(root, lang, source)
	for {
		match, ok := commandCursor.Next()
		if !ok {
			break
		}
		fn := match.CommandName
		if fn == nil {
			continue
		}
		text := strings.TrimSpace(fn.Text(source))
		if text == "" || commandPathPattern.MatchString(text) {
			continue
		}
		appendNodeTokens(lines, fn, core.TokenFunction, false, tokens)
	}
}

func appendNodeTokens(
	lines []string,
	node *gotreesitter.Node,
	typ core.TokenType,
	expandQuoted bool,
	tokens *[]core.Token,
) {
	if node == nil {
		return
	}

	start := node.StartPoint()
	end := node.EndPoint()
	startLine := int(start.Row)
	endLine := int(end.Row)
	if startLine > endLine {
		return
	}

	for line := startLine; line <= endLine; line++ {
		lineContent, ok := lineContentAt(lines, line)
		if !ok {
			continue
		}

		lineLen := len([]rune(lineContent))
		startCol := 0
		endCol := lineLen
		if line == startLine {
			startCol = min(int(start.Column), lineLen)
		}
		if line == endLine {
			endCol = min(int(end.Column), lineLen)
		}
		if expandQuoted && startLine == endLine {
			expandedStart, expandedEnd := expandQuotedRuneRange(lineContent, startCol, endCol)
			if expandedStart == startCol && expandedEnd == endCol && isDegenerateVariableString(lineContent, startCol, endCol) {
				return
			}
			startCol, endCol = expandedStart, expandedEnd
		}

		if endCol <= startCol {
			continue
		}

		*tokens = append(*tokens, core.Token{
			Line:     line,
			StartCol: startCol,
			EndCol:   endCol,
			Type:     typ,
			Priority: semanticPriority,
		})
	}
}

func expandQuotedRuneRange(line string, startCol, endCol int) (int, int) {
	runes := []rune(line)
	startCol = max(0, min(startCol, len(runes)))
	endCol = max(startCol, min(endCol, len(runes)))
	if startCol == 0 || endCol >= len(runes) {
		return startCol, endCol
	}

	quote := runes[startCol-1]
	if (quote != '"' && quote != '\'') || runes[endCol] != quote {
		return startCol, endCol
	}

	return startCol - 1, endCol + 1
}

func isDegenerateVariableString(line string, startCol, endCol int) bool {
	runes := []rune(line)
	startCol = max(0, min(startCol, len(runes)))
	endCol = max(startCol, min(endCol, len(runes)))
	if endCol <= startCol {
		return false
	}

	text := strings.TrimSpace(string(runes[startCol:endCol]))
	return strings.HasPrefix(text, "$")
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
