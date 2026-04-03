package batch

import (
	"strings"
	"testing"

	highlightcore "github.com/wharflab/tally/internal/highlight/core"
)

func TestTokenize_EmitsBatchTokens(t *testing.T) {
	t.Parallel()

	script := "REM install the app\nnet stop wuauserv /y\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenComment, "REM install the app")
	assertHasToken(t, script, tokens, highlightcore.TokenFunction, "net")
	assertHasToken(t, script, tokens, highlightcore.TokenParameter, "/y")
}

func TestTokenize_VariableReference(t *testing.T) {
	t.Parallel()

	script := "echo %PATH%\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenVariable, "%PATH%")
	assertHasToken(t, script, tokens, highlightcore.TokenFunction, "echo")
}

func TestTokenize_StringLiteral(t *testing.T) {
	t.Parallel()

	script := "echo \"hello world\"\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenString, "\"hello world\"")
}

func TestTokenize_SetAssignmentUsesStructuredQueryCaptures(t *testing.T) {
	t.Parallel()

	script := "set /p PATH=%PATH%;C:\\Tools\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenKeyword, "set")
	assertHasToken(t, script, tokens, highlightcore.TokenParameter, "/p")
	assertHasToken(t, script, tokens, highlightcore.TokenVariable, "PATH")
	assertHasToken(t, script, tokens, highlightcore.TokenString, "%PATH%;C:\\Tools")
}

func TestTokenize_RedirectOperator(t *testing.T) {
	t.Parallel()

	script := "dir > output.txt\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenFunction, "dir")
	assertHasToken(t, script, tokens, highlightcore.TokenOperator, ">")
}

func TestTokenize_ForVariable(t *testing.T) {
	t.Parallel()

	script := "for %%i in (*.txt) do echo %%i\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenVariable, "%%i")
}

func TestTokenize_DoesNotMarkPathInvocationAsFunction(t *testing.T) {
	t.Parallel()

	for _, script := range []string{
		"C:\\app\\tool.exe\n",
		"\\tools\\tool.exe\n",
		".\\bin\\tool.exe\n",
	} {
		tokens := Tokenize(script)

		assertNoToken(t, script, tokens, highlightcore.TokenFunction, strings.TrimSpace(script))
	}
}

func TestTokenize_UsesRuneColumns(t *testing.T) {
	t.Parallel()

	script := "echo ü \"%PATH%\"\n"
	tokens := Tokenize(script)

	assertHasToken(t, script, tokens, highlightcore.TokenString, "\"%PATH%\"")
}

func TestTokenize_BuiltinVariableReferenceUsesDefaultLibraryModifier(t *testing.T) {
	t.Parallel()

	script := "echo %CD%\n"
	tokens := Tokenize(script)

	assertHasTokenWithModifiers(t, script, tokens, highlightcore.TokenVariable, highlightcore.ModDefaultLibrary, "%CD%")
}

func TestTokenize_EmptyScript(t *testing.T) {
	t.Parallel()

	tokens := Tokenize("")
	if len(tokens) != 0 {
		t.Fatalf("Tokenize(\"\") returned %d tokens, want 0", len(tokens))
	}
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

func assertHasTokenWithModifiers(
	t *testing.T,
	script string,
	tokens []highlightcore.Token,
	wantType highlightcore.TokenType,
	wantModifiers uint32,
	wantText string,
) {
	t.Helper()

	for _, tok := range tokens {
		if tok.Type != wantType || tok.Priority != 30 && tok.Priority != 31 {
			continue
		}
		if tok.Modifiers != wantModifiers {
			continue
		}
		if got := tokenText(script, tok); got == wantText {
			return
		}
	}

	t.Fatalf(
		"missing token type=%s modifiers=%d text=%q in %+v",
		wantType,
		wantModifiers,
		wantText,
		tokens,
	)
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
