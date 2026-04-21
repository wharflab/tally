package highlight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/highlight/core"
)

func TestAnalyze_WindowsRunPathWinsOverShellFallback(t *testing.T) {
	t.Parallel()

	source := []byte(strings.Join([]string{
		"# escape=`",
		"FROM mcr.microsoft.com/windows/servercore:ltsc2025",
		"SHELL [\"C:\\\\Windows\\\\System32\\\\WindowsPowerShell\\\\v1.0\\\\powershell.exe\", \"-Command\"]",
		"RUN C:\\app\\tada.exe",
		"",
	}, "\n"))

	doc := Analyze("Dockerfile", source)
	if doc == nil {
		t.Fatal("Analyze() returned nil")
	}

	lineTokens := doc.LineTokens(3)
	assertHasLineToken(t, source, lineTokens, core.TokenString, "C:\\app\\tada.exe")
	assertNoLineToken(t, source, lineTokens, core.TokenFunction, "C:\\app\\tada.exe")
}

func assertHasLineToken(t *testing.T, source []byte, tokens []core.Token, wantType core.TokenType, wantText string) {
	t.Helper()

	lines := strings.Split(string(source), "\n")
	for _, tok := range tokens {
		if tok.Type != wantType || tok.Line < 0 || tok.Line >= len(lines) {
			continue
		}
		line := []rune(lines[tok.Line])
		if tok.StartCol < 0 || tok.EndCol < tok.StartCol || tok.EndCol > len(line) {
			continue
		}
		if got := string(line[tok.StartCol:tok.EndCol]); got == wantText {
			return
		}
	}

	t.Fatalf("missing token type=%s text=%q in %+v", wantType, wantText, tokens)
}

func TestAnalyze_WindowsCmdTokenization(t *testing.T) {
	t.Parallel()

	source := []byte(strings.Join([]string{
		"# escape=`",
		"FROM mcr.microsoft.com/windows/servercore:ltsc2025",
		"RUN net stop wuauserv /y",
		"RUN echo %PATH%",
		"",
	}, "\n"))

	doc := Analyze("Dockerfile", source)
	if doc == nil {
		t.Fatal("Analyze() returned nil")
	}

	line2 := doc.LineTokens(2)
	assertHasLineToken(t, source, line2, core.TokenFunction, "net")
	assertHasLineToken(t, source, line2, core.TokenParameter, "/y")

	line3 := doc.LineTokens(3)
	assertHasLineToken(t, source, line3, core.TokenVariable, "%PATH%")
}

func TestAnalyze_PowerShellLineContinuationTokenization(t *testing.T) {
	t.Parallel()

	source := []byte(strings.Join([]string{
		"# escape=`",
		"FROM mcr.microsoft.com/windows/servercore:ltsc2025",
		"SHELL [\"C:\\\\Windows\\\\System32\\\\WindowsPowerShell\\\\v1.0\\\\powershell.exe\", \"-Command\"]",
		"RUN New-Item -ItemType Directory -Path 'C:\\Program Files (x86)\\Microsoft Visual Studio\\Shared\\NuGetPackages' -Force | Out-Null; `",
		"    New-Item -ItemType Directory -Path 'C:\\Program Files\\dotnet\\sdk\\NuGetFallbackFolder' -Force | Out-Null",
		"",
	}, "\n"))

	doc := Analyze("Dockerfile", source)
	if doc == nil {
		t.Fatal("Analyze() returned nil")
	}

	// Line 3 (first line of RUN command) should have PowerShell tokens
	line3 := doc.LineTokens(3)
	assertHasLineToken(t, source, line3, core.TokenFunction, "New-Item")
	assertHasLineToken(t, source, line3, core.TokenParameter, "-ItemType")
	assertHasLineToken(t, source, line3, core.TokenParameter, "-Path")
	assertHasLineToken(t, source, line3, core.TokenParameter, "-Force")

	// Line 4 (continuation line) should also have PowerShell tokens
	line4 := doc.LineTokens(4)
	assertHasLineToken(t, source, line4, core.TokenFunction, "New-Item")
	assertHasLineToken(t, source, line4, core.TokenParameter, "-ItemType")
	assertHasLineToken(t, source, line4, core.TokenParameter, "-Path")
	assertHasLineToken(t, source, line4, core.TokenParameter, "-Force")
}

// TestAnalyze_RunHeredocWithMultiLineMounts reproduces the semantic-token gap
// seen in _tools/shellcheck-wasm/Dockerfile: when a RUN instruction has
// multiple --mount flags spread across backslash-continuation lines before a
// heredoc opener, the continuation lines before the opener must still receive
// flag/keyword tokens. They were previously being treated as heredoc body.
func TestAnalyze_RunHeredocWithMultiLineMounts(t *testing.T) {
	t.Parallel()

	source := []byte(strings.Join([]string{
		"ARG AST_GREP_VERSION=0.40.5",
		"FROM alpine",
		"RUN --mount=type=cache,target=/root/.npm,id=npm \\",
		"\t--mount=type=bind,source=rewrites,target=/rewrites,readonly \\",
		"\t<<EOF",
		"set -e",
		"./striptests",
		"EOF",
		"",
	}, "\n"))

	doc := Analyze("Dockerfile", source)
	if doc == nil {
		t.Fatal("Analyze() returned nil")
	}

	// First RUN flag line (line 2, the startLine of the instruction).
	line2 := doc.LineTokens(2)
	assertHasLineToken(t, source, line2, core.TokenKeyword, "RUN")
	assertHasLineToken(t, source, line2, core.TokenParameter, "--mount")

	// Continuation line that carries the second --mount flag must still be
	// tokenized — previously it was swallowed by the heredoc-body exclusion.
	line3 := doc.LineTokens(3)
	assertHasLineToken(t, source, line3, core.TokenParameter, "--mount")
	assertHasLineToken(t, source, line3, core.TokenProperty, "type")
	assertHasLineToken(t, source, line3, core.TokenString, "bind")

	// Heredoc opener line still gets `<<` and tag tokens. The tag is emitted
	// as a keyword so VS Code's fallback maps it to the grammar's
	// keyword.control.heredoc-token.shell.dockerfile scope.
	line4 := doc.LineTokens(4)
	assertHasLineToken(t, source, line4, core.TokenOperator, "<<")
	assertHasLineToken(t, source, line4, core.TokenKeyword, "EOF")

	// Closing terminator line emits a matching keyword token for the tag so
	// the grammar's heredoc-token scope theming applies there too. Source
	// layout: line 7 is the `EOF` terminator.
	line7 := doc.LineTokens(7)
	assertHasLineToken(t, source, line7, core.TokenKeyword, "EOF")
}

// TestAnalyze_ShellcheckWasmDockerfile exercises the semantic tokenizer on the
// real Dockerfile from _tools/shellcheck-wasm/ to make sure the in-repo file
// behaves the same as the synthetic test above. This is the file whose LSP
// coloring the maintainer inspected by eye.
func TestAnalyze_ShellcheckWasmDockerfile(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "_tools", "shellcheck-wasm", "Dockerfile")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	doc := Analyze(path, source)
	if doc == nil {
		t.Fatal("Analyze() returned nil")
	}

	lines := strings.Split(string(source), "\n")

	// Line indices are 0-based. The file has:
	//   line 36 (1-based 37): RUN --mount=type=cache,...
	//   line 37 (1-based 38): \t--mount=type=bind,source=rewrites,target=/rewrites,readonly \
	//   line 38 (1-based 39): \t<<EOF
	// Verify the offsets to guard against the file drifting.
	requireLinePrefix(t, lines, 36, "RUN --mount=type=cache")
	requireLinePrefix(t, lines, 37, "\t--mount=type=bind,source=rewrites")
	requireLinePrefix(t, lines, 38, "\t<<EOF")

	runLine := doc.LineTokens(36)
	assertHasLineToken(t, source, runLine, core.TokenKeyword, "RUN")
	assertHasLineToken(t, source, runLine, core.TokenParameter, "--mount")

	// The second --mount flag sits on a continuation line between the RUN
	// startLine and the heredoc opener. The LSP must emit flag/kv tokens for
	// it; previously it was excluded as heredoc body.
	secondMount := doc.LineTokens(37)
	assertHasLineToken(t, source, secondMount, core.TokenParameter, "--mount")
	assertHasLineToken(t, source, secondMount, core.TokenProperty, "type")
	assertHasLineToken(t, source, secondMount, core.TokenString, "bind")
	assertHasLineToken(t, source, secondMount, core.TokenProperty, "source")
	assertHasLineToken(t, source, secondMount, core.TokenString, "rewrites")

	openerLine := doc.LineTokens(38)
	assertHasLineToken(t, source, openerLine, core.TokenOperator, "<<")
	assertHasLineToken(t, source, openerLine, core.TokenKeyword, "EOF")

	// The matching terminator line (105, 0-based) reads `EOF` on its own.
	// Verify we emit a keyword token there too so the grammar's closing tag
	// styling still applies under semantic highlighting.
	requireLinePrefix(t, lines, 105, "EOF")
	terminatorLine := doc.LineTokens(105)
	assertHasLineToken(t, source, terminatorLine, core.TokenKeyword, "EOF")
}

func requireLinePrefix(t *testing.T, lines []string, line int, prefix string) {
	t.Helper()
	if line < 0 || line >= len(lines) {
		t.Fatalf("line %d out of range (have %d lines)", line, len(lines))
	}
	if !strings.HasPrefix(lines[line], prefix) {
		t.Fatalf("line %d prefix mismatch: got %q want prefix %q", line, lines[line], prefix)
	}
}

func assertNoLineToken(t *testing.T, source []byte, tokens []core.Token, wantType core.TokenType, wantText string) {
	t.Helper()

	lines := strings.Split(string(source), "\n")
	for _, tok := range tokens {
		if tok.Type != wantType || tok.Line < 0 || tok.Line >= len(lines) {
			continue
		}
		line := []rune(lines[tok.Line])
		if tok.StartCol < 0 || tok.EndCol < tok.StartCol || tok.EndCol > len(line) {
			continue
		}
		if got := string(line[tok.StartCol:tok.EndCol]); got == wantText {
			t.Fatalf("unexpected token type=%s text=%q in %+v", wantType, wantText, tokens)
		}
	}
}
