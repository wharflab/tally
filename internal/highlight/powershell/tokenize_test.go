package powershell

import (
	"testing"

	highlightcore "github.com/wharflab/tally/internal/highlight/core"
)

func TestTokenize_EmitsPowerShellTokens(t *testing.T) {
	t.Parallel()

	script := "Invoke-WebRequest \"https://example.com/app.tar.gz\" -OutFile \"$HOME/app.tar.gz\" # note\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenFunction, "Invoke-WebRequest")
	assertHasToken(t, script, tokens, highlightcore.TokenString, "\"https://example.com/app.tar.gz\"")
	assertHasToken(t, script, tokens, highlightcore.TokenParameter, "-OutFile")
	assertHasToken(t, script, tokens, highlightcore.TokenVariable, "$HOME")
	assertHasToken(t, script, tokens, highlightcore.TokenComment, "# note")
}

func TestTokenize_UsesRuneColumns(t *testing.T) {
	t.Parallel()

	script := "Write-Host ü \"$HOME\"\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenString, "\"$HOME\"")
	assertHasToken(t, script, tokens, highlightcore.TokenVariable, "$HOME")
}

func TestTokenize_DoesNotMarkWindowsPathAsFunction(t *testing.T) {
	t.Parallel()

	script := "C:\\app\\tada.exe\n"
	tokens := Tokenize(script)

	assertNoToken(t, script, tokens, highlightcore.TokenFunction, "C:\\app\\tada.exe")
}

func assertHasToken(
	t *testing.T,
	script string,
	tokens []highlightcore.Token,
	wantType highlightcore.TokenType,
	wantText string,
) {
	t.Helper()

	for _, tok := range tokens {
		if tok.Type != wantType || tok.Priority != 30 {
			continue
		}
		if got := tokenText(script, tok); got == wantText {
			return
		}
	}

	t.Fatalf("missing token type=%s priority=%d text=%q in %+v", wantType, 30, wantText, tokens)
}

func assertNoToken(t *testing.T, script string, tokens []highlightcore.Token, wantType highlightcore.TokenType, wantText string) {
	t.Helper()
	for _, tok := range tokens {
		if tok.Type != wantType {
			continue
		}
		if got := tokenText(script, tok); got == wantText {
			t.Fatalf("unexpected token type=%s text=%q in %+v", wantType, wantText, tokens)
		}
	}
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
