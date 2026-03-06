package dockerfile

import (
	"bytes"
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/sourcemap"
)

func TestInstructionKeywordToken_UsesRuneColumns(t *testing.T) {
	t.Parallel()
	line := "FROM alpine"
	tok, ok := instructionKeywordToken(line, 0)
	if !ok {
		t.Fatal("expected instruction token")
	}
	assertTokenText(t, line, tok, "FROM")
}

func TestRegexTokenizers_UseRuneColumns(t *testing.T) {
	t.Parallel()

	line := "RUN echo ü --mount=type=cache,target=/tmp --label='välüe' $HOME 123 <<EOF"

	assertTokenText(t, line, flagTokens(line, 0)[0], "--mount")
	assertTokenText(t, line, quotedTokens(line, 0, '\\')[0], "'välüe'")
	assertTokenText(t, line, variableTokens(line, 0)[0], "$HOME")
	assertTokenText(t, line, numberTokens(line, 0)[0], "123")

	heredoc := heredocTokens(line, 0)
	if len(heredoc) != 2 {
		t.Fatalf("expected 2 heredoc tokens, got %d", len(heredoc))
	}
	assertTokenText(t, line, heredoc[0], "<<")
	assertTokenText(t, line, heredoc[1], "EOF")
}

func TestKVValueTokens_UseRuneColumns(t *testing.T) {
	t.Parallel()

	line := "RUN echo ü --mount=type=cache,target=/tmp"
	base := strings.Index(line, "type=cache,target=/tmp")
	tokens := kvValueTokens(line, "type=cache,target=/tmp", 0, base)
	if len(tokens) != 4 {
		t.Fatalf("expected 4 kv tokens, got %d", len(tokens))
	}
	assertTokenText(t, line, tokens[0], "type")
	assertTokenText(t, line, tokens[1], "cache")
	assertTokenText(t, line, tokens[2], "target")
	assertTokenText(t, line, tokens[3], "/tmp")
}

func TestQuotedTokens_RespectEscapeDirective(t *testing.T) {
	t.Parallel()

	line := "ENV NAME=\"say `\"hello`\"\""
	tokens := quotedTokens(line, 0, '`')
	if len(tokens) != 1 {
		t.Fatalf("expected 1 quoted token, got %d", len(tokens))
	}
	assertTokenText(t, line, tokens[0], "\"say `\"hello`\"\"")
}

func TestTokenize_WindowsPaths(t *testing.T) {
	t.Parallel()

	source := []byte(strings.Join([]string{
		"# escape=`",
		"ENV TEMP=C:\\Temp",
		"ENV PATH=\"C:\\Program Files\\dotnet;${PATH}\"",
		"SHELL [\"C:\\\\Windows\\\\System32\\\\WindowsPowerShell\\\\v1.0\\\\powershell.exe\", \"-Command\"]",
		"RUN .\\PortableGit.exe",
		"RUN C:\\app\\tada.exe",
		"",
	}, "\n"))
	sm := sourcemap.New(source)
	result, err := parser.Parse(bytes.NewReader(source))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tokens := Tokenize(sm, result.AST, '`')

	assertHasTokenText(t, string(source), tokens, core.TokenString, "C:\\Temp")
	assertHasTokenText(t, string(source), tokens, core.TokenString, "\"C:\\Program Files\\dotnet;${PATH}\"")
	assertHasTokenText(t, string(source), tokens, core.TokenVariable, "${PATH}")
	assertHasTokenText(
		t,
		string(source),
		tokens,
		core.TokenString,
		"\"C:\\\\Windows\\\\System32\\\\WindowsPowerShell\\\\v1.0\\\\powershell.exe\"",
	)
	assertHasTokenText(t, string(source), tokens, core.TokenString, ".\\PortableGit.exe")
	assertHasTokenText(t, string(source), tokens, core.TokenString, "C:\\app\\tada.exe")
}

func TestTokenize_FallbackIncludesNumberTokens(t *testing.T) {
	t.Parallel()

	source := []byte("RUN echo 123\n")
	sm := sourcemap.New(source)

	tokens := Tokenize(sm, nil, '\\')

	assertHasTokenText(t, string(source), tokens, core.TokenNumber, "123")
}

func TestTokenize_OnlyHighlightsStageAliasOnFrom(t *testing.T) {
	t.Parallel()

	source := []byte("FROM alpine AS build\nRUN echo AS value\n")
	sm := sourcemap.New(source)
	result, err := parser.Parse(bytes.NewReader(source))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tokens := Tokenize(sm, result.AST, '\\')

	assertHasTokenText(t, string(source), tokens, core.TokenKeyword, "FROM")
	assertHasTokenText(t, string(source), tokens, core.TokenKeyword, "AS")
	assertHasTokenText(t, string(source), tokens, core.TokenVariable, "build")
	assertNoTokenText(t, string(source), tokens, core.TokenVariable, "value")
}

func assertTokenText(t *testing.T, line string, tok core.Token, want string) {
	t.Helper()
	got := string([]rune(line)[tok.StartCol:tok.EndCol])
	if got != want {
		t.Fatalf("token text = %q, want %q (cols %d:%d)", got, want, tok.StartCol, tok.EndCol)
	}
}

func assertHasTokenText(t *testing.T, source string, tokens []core.Token, wantType core.TokenType, want string) {
	t.Helper()

	lines := strings.Split(source, "\n")
	for _, tok := range tokens {
		if tok.Type != wantType || tok.Line < 0 || tok.Line >= len(lines) {
			continue
		}
		line := lines[tok.Line]
		if tok.StartCol < 0 || tok.EndCol < tok.StartCol || tok.EndCol > len([]rune(line)) {
			continue
		}
		if got := string([]rune(line)[tok.StartCol:tok.EndCol]); got == want {
			return
		}
	}

	t.Fatalf("missing token type=%s text=%q in %+v", wantType, want, tokens)
}

func assertNoTokenText(t *testing.T, source string, tokens []core.Token, wantType core.TokenType, want string) {
	t.Helper()

	lines := strings.Split(source, "\n")
	for _, tok := range tokens {
		if tok.Type != wantType || tok.Line < 0 || tok.Line >= len(lines) {
			continue
		}
		line := lines[tok.Line]
		if tok.StartCol < 0 || tok.EndCol < tok.StartCol || tok.EndCol > len([]rune(line)) {
			continue
		}
		if got := string([]rune(line)[tok.StartCol:tok.EndCol]); got == want {
			t.Fatalf("unexpected token type=%s text=%q in %+v", wantType, want, tokens)
		}
	}
}
