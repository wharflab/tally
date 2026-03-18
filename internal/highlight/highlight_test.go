package highlight

import (
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
		"RUN New-Item -ItemType Directory -Path 'C:\\Program Files (x86)\\Microsoft Visual Studio\\Shared\\NuGetPackages' -Force | Out-Null\"; `",
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
