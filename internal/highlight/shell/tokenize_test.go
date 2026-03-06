package shell

import (
	"testing"

	highlightcore "github.com/wharflab/tally/internal/highlight/core"
	myshell "github.com/wharflab/tally/internal/shell"
)

func TestTokenize_ASTUsesRuneColumns(t *testing.T) {
	t.Parallel()

	script := "echo ü \"$HOME\"\n"
	tokens := Tokenize(script, myshell.VariantBash)

	assertHasToken(t, script, tokens, highlightcore.TokenString, 30, "\"$HOME\"")
	assertHasToken(t, script, tokens, highlightcore.TokenVariable, 30, "$HOME")
}

func TestTokenize_LexicalFallbackUsesRuneColumns(t *testing.T) {
	t.Parallel()

	script := "printf ü '${HOME}' \"$HOME\"\n"
	tokens := Tokenize(script, myshell.VariantUnknown)

	assertHasToken(t, script, tokens, highlightcore.TokenString, 25, "\"$HOME\"")
	assertHasToken(t, script, tokens, highlightcore.TokenVariable, 26, "$HOME")
}

func TestTokenize_ASTRejectsMultilineQuotedStringToken(t *testing.T) {
	t.Parallel()

	script := "echo \"hello\nworld\"\n"
	tokens := Tokenize(script, myshell.VariantBash)

	for _, tok := range tokens {
		if tok.Type == highlightcore.TokenString && tok.Priority == 30 {
			t.Fatalf("unexpected multiline AST string token: %+v", tok)
		}
	}
}

func assertHasToken(
	t *testing.T,
	script string,
	tokens []highlightcore.Token,
	wantType highlightcore.TokenType,
	wantPriority int,
	wantText string,
) {
	t.Helper()

	for _, tok := range tokens {
		if tok.Type != wantType || tok.Priority != wantPriority {
			continue
		}
		if got := tokenText(script, tok); got == wantText {
			return
		}
	}

	t.Fatalf("missing token type=%s priority=%d text=%q in %+v", wantType, wantPriority, wantText, tokens)
}

func tokenText(script string, tok highlightcore.Token) string {
	lines := splitLines(script)
	if tok.Line < 0 || tok.Line >= len(lines) {
		return ""
	}
	line := []rune(lines[tok.Line])
	if tok.StartCol < 0 || tok.StartCol > len(line) {
		return ""
	}
	if tok.EndCol < tok.StartCol {
		return ""
	}
	if tok.EndCol > len(line) {
		tok.EndCol = len(line)
	}
	return string(line[tok.StartCol:tok.EndCol])
}

func splitLines(script string) []string {
	lines := make([]string, 0, 8)
	start := 0
	for i, r := range script {
		if r != '\n' {
			continue
		}
		lines = append(lines, script[start:i])
		start = i + 1
	}
	if start <= len(script) {
		lines = append(lines, script[start:])
	}
	return lines
}
