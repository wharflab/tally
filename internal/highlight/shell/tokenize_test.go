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

func TestTokenize_PowerShellUsesParserBackedPath(t *testing.T) {
	t.Parallel()

	script := "Invoke-WebRequest \"https://example.com/app.tar.gz\" -OutFile \"$HOME/app.tar.gz\"\n"
	tokens := Tokenize(script, myshell.VariantPowerShell)

	assertHasToken(t, script, tokens, highlightcore.TokenFunction, 30, "Invoke-WebRequest")
	assertHasToken(t, script, tokens, highlightcore.TokenParameter, 30, "-OutFile")
	assertHasToken(t, script, tokens, highlightcore.TokenVariable, 30, "$HOME")
}

func TestTokenize_PowerShellEmptyResultRemainsEmpty(t *testing.T) {
	t.Parallel()

	script := "C:\\app\\tool.exe\n"
	tokens := Tokenize(script, myshell.VariantPowerShell)

	if len(tokens) != 0 {
		t.Fatalf("Tokenize() returned %d tokens, want 0: %+v", len(tokens), tokens)
	}
}

func TestTokenize_CmdUsesParserBackedPath(t *testing.T) {
	t.Parallel()

	script := "REM install\nnet stop wuauserv /y\n"
	tokens := Tokenize(script, myshell.VariantCmd)

	assertHasToken(t, script, tokens, highlightcore.TokenComment, 30, "REM install")
	assertHasToken(t, script, tokens, highlightcore.TokenFunction, 30, "net")
	assertHasToken(t, script, tokens, highlightcore.TokenParameter, 30, "/y")
}

func TestTokenize_CmdVariableReference(t *testing.T) {
	t.Parallel()

	script := "echo %PATH%\n"
	tokens := Tokenize(script, myshell.VariantCmd)

	assertHasToken(t, script, tokens, highlightcore.TokenVariable, 30, "%PATH%")
}

func TestTokenize_CmdPathInvocationNotFunction(t *testing.T) {
	t.Parallel()

	script := "C:\\app\\tool.exe\n"
	tokens := Tokenize(script, myshell.VariantCmd)

	for _, tok := range tokens {
		if tok.Type == highlightcore.TokenFunction && tok.Priority == 30 {
			text := tokenText(script, tok)
			if text == "C:\\app\\tool.exe" {
				t.Fatalf("unexpected function token for path invocation: %+v", tok)
			}
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
