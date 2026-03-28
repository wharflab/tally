package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferCanonicalStopsignalRule_Metadata(t *testing.T) {
	t.Parallel()
	r := NewPreferCanonicalStopsignalRule()
	meta := r.Metadata()

	if meta.Code != PreferCanonicalStopsignalRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, PreferCanonicalStopsignalRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "style" {
		t.Errorf("Category = %q, want style", meta.Category)
	}
}

func TestPreferCanonicalStopsignalRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferCanonicalStopsignalRule(), []testutil.RuleTestCase{
		// --- Violations (non-canonical) ---
		{
			Name:           "quoted signal — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL \"SIGINT\"\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGINT"},
		},
		{
			Name:           "missing SIG prefix TERM — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL TERM\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGTERM"},
		},
		{
			Name:           "missing SIG prefix QUIT — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL QUIT\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGQUIT"},
		},
		{
			Name:           "missing SIG prefix INT — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL INT\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGINT"},
		},
		{
			Name:           "non-canonical RT signal RTMIN+3 — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL RTMIN+3\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGRTMIN+3"},
		},
		{
			Name:           "numeric 15 (SIGTERM) — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL 15\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGTERM"},
		},
		{
			Name:           "numeric 2 (SIGINT) — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL 2\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGINT"},
		},
		{
			Name:           "numeric 9 (SIGKILL) — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL 9\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGKILL"},
		},
		{
			Name:           "lowercase sigterm — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL sigterm\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGTERM"},
		},
		{
			Name:           "lowercase sigquit — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL sigquit\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGQUIT"},
		},
		{
			Name:           "mixed case SigInt — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SigInt\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGINT"},
		},
		{
			Name:           "missing SIG prefix KILL — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL KILL\n",
			WantViolations: 1,
			WantMessages:   []string{"should be written as SIGKILL"},
		},

		// --- No violations (already canonical) ---
		{
			Name:           "SIGTERM — no violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGTERM\n",
			WantViolations: 0,
		},
		{
			Name:           "SIGQUIT — no violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGQUIT\n",
			WantViolations: 0,
		},
		{
			Name:           "SIGINT — no violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGINT\n",
			WantViolations: 0,
		},
		{
			Name:           "SIGKILL — no violation (canonical, even though ungraceful)",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGKILL\n",
			WantViolations: 0,
		},
		{
			Name:           "SIGRTMIN+3 — no violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGRTMIN+3\n",
			WantViolations: 0,
		},
		{
			Name:           "SIGWINCH — no violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGWINCH\n",
			WantViolations: 0,
		},
		{
			Name:           "SIGHUP — no violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGHUP\n",
			WantViolations: 0,
		},
		{
			Name:           "unknown numeric 42 — no violation (can't canonicalize)",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL 42\n",
			WantViolations: 0,
		},

		// --- Skipped ---
		{
			Name:           "env var reference — skipped",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL $MY_SIGNAL\n",
			WantViolations: 0,
		},
		{
			Name:           "env var with braces — skipped",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL ${STOP_SIG}\n",
			WantViolations: 0,
		},
		{
			Name:           "no STOPSIGNAL — no violation",
			Content:        "FROM alpine:3.20\nRUN echo hello\n",
			WantViolations: 0,
		},

		// --- Windows stages (skipped) ---
		{
			Name: "Windows stage — skipped",
			Content: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"STOPSIGNAL TERM\n",
			WantViolations: 0,
		},
		{
			Name: "Windows stage with numeric — skipped",
			Content: "FROM mcr.microsoft.com/windows/nanoserver:ltsc2022\n" +
				"STOPSIGNAL 15\n",
			WantViolations: 0,
		},

		// --- Multi-stage ---
		{
			Name: "multi-stage — violation in one stage only",
			Content: "FROM alpine:3.20 AS builder\n" +
				"STOPSIGNAL TERM\n" +
				"\n" +
				"FROM nginx:1.27\n" +
				"STOPSIGNAL SIGQUIT\n",
			WantViolations: 1,
			WantMessages:   []string{"TERM should be written as SIGTERM"},
		},
		{
			Name: "multi-stage — violations in both stages",
			Content: "FROM alpine:3.20 AS builder\n" +
				"STOPSIGNAL TERM\n" +
				"\n" +
				"FROM postgres:16\n" +
				"STOPSIGNAL 2\n",
			WantViolations: 2,
		},
		{
			Name: "multi-stage — Linux violation, Windows skipped",
			Content: "FROM alpine:3.20 AS builder\n" +
				"STOPSIGNAL TERM\n" +
				"\n" +
				"FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"STOPSIGNAL TERM\n",
			WantViolations: 1,
		},
	})
}

func TestPreferCanonicalStopsignalRule_SuggestedFix(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nSTOPSIGNAL TERM\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCanonicalStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}
	if v.SuggestedFix.Safety != rules.FixSafe {
		t.Errorf("Safety = %v, want FixSafe", v.SuggestedFix.Safety)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}

	edit := v.SuggestedFix.Edits[0]
	if edit.NewText != "SIGTERM" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "SIGTERM")
	}
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit Start.Line = %d, want 2", edit.Location.Start.Line)
	}
	// "STOPSIGNAL " is 11 chars, signal starts at column 11
	if edit.Location.Start.Column != 11 {
		t.Errorf("edit Start.Column = %d, want 11", edit.Location.Start.Column)
	}
	// "TERM" is 4 chars, so end column = 15
	if edit.Location.End.Column != 15 {
		t.Errorf("edit End.Column = %d, want 15", edit.Location.End.Column)
	}
}

func TestPreferCanonicalStopsignalRule_FixQuoted(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nSTOPSIGNAL \"SIGINT\"\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCanonicalStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}

	edit := v.SuggestedFix.Edits[0]
	if edit.NewText != "SIGINT" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "SIGINT")
	}
	// "STOPSIGNAL " = 11, "\"SIGINT\"" starts at 11, ends at 19 (includes quotes)
	if edit.Location.Start.Column != 11 {
		t.Errorf("edit Start.Column = %d, want 11", edit.Location.Start.Column)
	}
	if edit.Location.End.Column != 19 {
		t.Errorf("edit End.Column = %d, want 19", edit.Location.End.Column)
	}
}

func TestPreferCanonicalStopsignalRule_FixNumeric(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nSTOPSIGNAL 15\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCanonicalStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	edit := violations[0].SuggestedFix.Edits[0]
	if edit.NewText != "SIGTERM" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "SIGTERM")
	}
	// "STOPSIGNAL " = 11, "15" is 2 chars
	if edit.Location.Start.Column != 11 {
		t.Errorf("edit Start.Column = %d, want 11", edit.Location.Start.Column)
	}
	if edit.Location.End.Column != 13 {
		t.Errorf("edit End.Column = %d, want 13", edit.Location.End.Column)
	}
}

func TestPreferCanonicalStopsignalRule_FixRTSignal(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nSTOPSIGNAL RTMIN+3\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCanonicalStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	edit := violations[0].SuggestedFix.Edits[0]
	if edit.NewText != "SIGRTMIN+3" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "SIGRTMIN+3")
	}
}

func TestPreferCanonicalStopsignalRule_ViolationLocation(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nRUN echo hello\nSTOPSIGNAL TERM\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCanonicalStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Location.Start.Line != 3 {
		t.Errorf("Start.Line = %d, want 3", violations[0].Location.Start.Line)
	}
}
