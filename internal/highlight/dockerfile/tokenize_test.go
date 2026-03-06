package dockerfile

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/highlight/core"
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
	assertTokenText(t, line, quotedTokens(line, 0)[0], "'välüe'")
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

func assertTokenText(t *testing.T, line string, tok core.Token, want string) {
	t.Helper()
	got := string([]rune(line)[tok.StartCol:tok.EndCol])
	if got != want {
		t.Fatalf("token text = %q, want %q (cols %d:%d)", got, want, tok.StartCol, tok.EndCol)
	}
}
