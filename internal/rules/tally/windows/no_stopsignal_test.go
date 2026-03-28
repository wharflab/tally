package windows

import (
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoStopsignalRule_Metadata(t *testing.T) {
	t.Parallel()
	snaps.MatchStandaloneJSON(t, NewNoStopsignalRule().Metadata())
}

func TestNoStopsignalRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoStopsignalRule(), []testutil.RuleTestCase{
		{
			Name: "stopsignal on windows servercore",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
STOPSIGNAL SIGTERM
`,
			WantViolations: 1,
			WantCodes:      []string{NoStopsignalRuleCode},
			WantMessages:   []string{"SIGTERM has no effect on Windows"},
		},
		{
			Name: "stopsignal on windows nanoserver",
			Content: `FROM mcr.microsoft.com/windows/nanoserver:ltsc2022
STOPSIGNAL SIGINT
`,
			WantViolations: 1,
			WantMessages:   []string{"SIGINT has no effect on Windows"},
		},
		{
			Name: "stopsignal sigkill on windows",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
STOPSIGNAL SIGKILL
`,
			WantViolations: 1,
			WantMessages:   []string{"SIGKILL has no effect on Windows"},
		},
		{
			Name: "stopsignal numeric on windows",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
STOPSIGNAL 9
`,
			WantViolations: 1,
			WantMessages:   []string{"9 has no effect on Windows"},
		},
		{
			Name: "stopsignal on linux no violation",
			Content: `FROM alpine:3.20
STOPSIGNAL SIGTERM
`,
			WantViolations: 0,
		},
		{
			Name: "stopsignal on ubuntu no violation",
			Content: `FROM ubuntu:22.04
STOPSIGNAL SIGKILL
`,
			WantViolations: 0,
		},
		{
			Name: "mixed stages only windows flagged",
			Content: `FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
STOPSIGNAL SIGTERM
RUN dotnet publish -c Release -o /app

FROM mcr.microsoft.com/windows/servercore:ltsc2022
STOPSIGNAL SIGTERM
CMD ["dotnet", "/app/MyApp.dll"]
`,
			WantViolations: 1,
			WantMessages:   []string{"SIGTERM has no effect on Windows"},
		},
		{
			Name: "multiple stopsignal in same windows stage",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
STOPSIGNAL SIGTERM
RUN powershell -Command echo hello
STOPSIGNAL SIGINT
`,
			WantViolations: 2,
		},
		{
			Name: "windows stage without stopsignal no violation",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN powershell -Command echo hello
CMD ["cmd", "/C", "echo", "hi"]
`,
			WantViolations: 0,
		},
		{
			Name: "empty scratch no violation",
			Content: `FROM scratch
`,
			WantViolations: 0,
		},
		{
			Name: "windows platform flag with stopsignal",
			Content: `FROM --platform=windows/amd64 mcr.microsoft.com/dotnet/sdk:8.0
STOPSIGNAL SIGTERM
`,
			WantViolations: 1,
		},
	})
}

func TestNoStopsignalRule_Fix(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nSTOPSIGNAL SIGTERM\nCMD [\"cmd\", \"/C\", \"echo\", \"hi\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]

	// Should have two alternative fixes
	allFixes := v.AllFixes()
	if len(allFixes) != 2 {
		t.Fatalf("expected 2 fix alternatives, got %d", len(allFixes))
	}

	// Preferred fix: comment out (backward compat via SuggestedFix)
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}
	if v.SuggestedFix.Safety != rules.FixSafe {
		t.Errorf("Safety = %v, want FixSafe", v.SuggestedFix.Safety)
	}
	if !v.SuggestedFix.IsPreferred {
		t.Error("expected IsPreferred to be true")
	}
	if v.SuggestedFix.Priority != -1 {
		t.Errorf("Priority = %d, want -1", v.SuggestedFix.Priority)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}

	edit := v.SuggestedFix.Edits[0]
	wantText := "# [commented out by tally - STOPSIGNAL has no effect on Windows containers]: STOPSIGNAL SIGTERM"
	if edit.NewText != wantText {
		t.Errorf("NewText = %q, want %q", edit.NewText, wantText)
	}
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit Start.Line = %d, want 2", edit.Location.Start.Line)
	}
	if edit.Location.Start.Column != 0 {
		t.Errorf("edit Start.Column = %d, want 0", edit.Location.Start.Column)
	}
	if edit.Location.End.Column != 18 {
		t.Errorf("edit End.Column = %d, want 18", edit.Location.End.Column)
	}

	// Alternative fix: delete
	deleteFix := allFixes[1]
	if deleteFix.Safety != rules.FixSuggestion {
		t.Errorf("delete fix Safety = %v, want FixSuggestion", deleteFix.Safety)
	}
	if deleteFix.IsPreferred {
		t.Error("delete fix should not be preferred")
	}
	if deleteFix.Edits[0].NewText != "" {
		t.Errorf("delete fix NewText = %q, want empty string", deleteFix.Edits[0].NewText)
	}
}

func TestNoStopsignalRule_FixPreservesSignalValue(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nSTOPSIGNAL SIGKILL\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	edit := violations[0].SuggestedFix.Edits[0]
	wantText := "# [commented out by tally - STOPSIGNAL has no effect on Windows containers]: STOPSIGNAL SIGKILL"
	if edit.NewText != wantText {
		t.Errorf("NewText = %q, want %q", edit.NewText, wantText)
	}
}

func TestNoStopsignalRule_ViolationLocation(t *testing.T) {
	t.Parallel()

	content := "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nRUN echo hello\nSTOPSIGNAL SIGTERM\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Location.Start.Line != 3 {
		t.Errorf("Start.Line = %d, want 3", violations[0].Location.Start.Line)
	}
}
