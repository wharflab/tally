package directive

import "testing"

func TestLexComment_TallyIgnoreWithReason(t *testing.T) {
	t.Parallel()

	text := "# tally global ignore=max-lines;reason=kept for compatibility"
	tokens := LexComment(text)

	assertLexToken(t, text, tokens, CommentTokenKeyword, "tally")
	assertLexToken(t, text, tokens, CommentTokenKeyword, "global")
	assertLexToken(t, text, tokens, CommentTokenKeyword, "ignore")
	assertLexToken(t, text, tokens, CommentTokenRule, "max-lines")
	assertLexToken(t, text, tokens, CommentTokenKeyword, "reason")
	assertLexToken(t, text, tokens, CommentTokenValue, "kept for compatibility")
}

func TestLexComment_TallyIgnoreReasonAllowsSemicolons(t *testing.T) {
	t.Parallel()

	text := "# tally ignore=DL3006;reason=kept;for later"
	tokens := LexComment(text)

	assertLexToken(t, text, tokens, CommentTokenKeyword, "tally")
	assertLexToken(t, text, tokens, CommentTokenKeyword, "ignore")
	assertLexToken(t, text, tokens, CommentTokenRule, "DL3006")
	assertLexToken(t, text, tokens, CommentTokenKeyword, "reason")
	assertLexToken(t, text, tokens, CommentTokenValue, "kept;for later")
}

func TestLexComment_BuildxAndShell(t *testing.T) {
	t.Parallel()

	buildx := LexComment("# check=skip=DL3006,DL3008")
	assertLexToken(t, "# check=skip=DL3006,DL3008", buildx, CommentTokenKeyword, "check")
	assertLexToken(t, "# check=skip=DL3006,DL3008", buildx, CommentTokenKeyword, "skip")
	assertLexToken(t, "# check=skip=DL3006,DL3008", buildx, CommentTokenRule, "DL3006")
	assertLexToken(t, "# check=skip=DL3006,DL3008", buildx, CommentTokenRule, "DL3008")

	shell := LexComment("# hadolint shell=cmd.exe")
	assertLexToken(t, "# hadolint shell=cmd.exe", shell, CommentTokenKeyword, "hadolint")
	assertLexToken(t, "# hadolint shell=cmd.exe", shell, CommentTokenKeyword, "shell")
	assertLexToken(t, "# hadolint shell=cmd.exe", shell, CommentTokenValue, "cmd.exe")
}

func assertLexToken(t *testing.T, text string, tokens []CommentToken, wantKind CommentTokenKind, wantText string) {
	t.Helper()

	for _, tok := range tokens {
		if tok.Kind != wantKind {
			continue
		}
		if tok.StartByte < 0 || tok.EndByte > len(text) || tok.EndByte < tok.StartByte {
			continue
		}
		if got := text[tok.StartByte:tok.EndByte]; got == wantText {
			return
		}
	}

	t.Fatalf("missing token kind=%d text=%q in %+v", wantKind, wantText, tokens)
}
