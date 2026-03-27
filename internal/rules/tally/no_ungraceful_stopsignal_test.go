package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestNoUngracefulStopsignalRule_Metadata(t *testing.T) {
	t.Parallel()
	r := NewNoUngracefulStopsignalRule()
	meta := r.Metadata()

	if meta.Code != NoUngracefulStopsignalRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, NoUngracefulStopsignalRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want correctness", meta.Category)
	}
}

func TestNoUngracefulStopsignalRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewNoUngracefulStopsignalRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name:           "SIGKILL — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGKILL\n",
			WantViolations: 1,
			WantMessages:   []string{"SIGKILL is not a graceful stop signal"},
		},
		{
			Name:           "SIGSTOP — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL SIGSTOP\n",
			WantViolations: 1,
			WantMessages:   []string{"SIGSTOP is not a graceful stop signal"},
		},
		{
			Name:           "numeric 9 (SIGKILL) — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL 9\n",
			WantViolations: 1,
			WantMessages:   []string{"SIGKILL"},
		},
		{
			Name:           "numeric 19 (SIGSTOP) — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL 19\n",
			WantViolations: 1,
			WantMessages:   []string{"SIGSTOP"},
		},
		{
			Name:           "KILL without SIG prefix — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL KILL\n",
			WantViolations: 1,
			WantMessages:   []string{"SIGKILL"},
		},
		{
			Name:           "STOP without SIG prefix — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL STOP\n",
			WantViolations: 1,
			WantMessages:   []string{"SIGSTOP"},
		},
		{
			Name:           "lowercase sigkill — violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL sigkill\n",
			WantViolations: 1,
			WantMessages:   []string{"SIGKILL"},
		},

		// --- No violations ---
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
			Name:           "numeric 15 (SIGTERM) — no violation",
			Content:        "FROM alpine:3.20\nSTOPSIGNAL 15\n",
			WantViolations: 0,
		},
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

		// --- Multi-stage ---
		{
			Name: "multi-stage — violation in one stage only",
			Content: "FROM alpine:3.20 AS builder\n" +
				"STOPSIGNAL SIGKILL\n" +
				"RUN echo build\n" +
				"\n" +
				"FROM nginx:1.27\n" +
				"STOPSIGNAL SIGQUIT\n",
			WantViolations: 1,
			WantMessages:   []string{"SIGKILL"},
		},
		{
			Name: "multi-stage — violations in both stages",
			Content: "FROM alpine:3.20 AS builder\n" +
				"STOPSIGNAL SIGKILL\n" +
				"\n" +
				"FROM postgres:16\n" +
				"STOPSIGNAL SIGSTOP\n",
			WantViolations: 2,
		},
	})
}

func TestNoUngracefulStopsignalRule_SuggestedFix(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nSTOPSIGNAL SIGKILL\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoUngracefulStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}
	if v.SuggestedFix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
	}
	if !v.SuggestedFix.IsPreferred {
		t.Error("expected IsPreferred to be true")
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
	// "SIGKILL" is 7 chars, so end column = 18
	if edit.Location.End.Column != 18 {
		t.Errorf("edit End.Column = %d, want 18", edit.Location.End.Column)
	}
}

func TestNoUngracefulStopsignalRule_FixNumeric(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nSTOPSIGNAL 9\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoUngracefulStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}
	edit := v.SuggestedFix.Edits[0]
	if edit.NewText != "SIGTERM" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "SIGTERM")
	}
	// "STOPSIGNAL " = 11, "9" ends at 12
	if edit.Location.Start.Column != 11 {
		t.Errorf("edit Start.Column = %d, want 11", edit.Location.Start.Column)
	}
	if edit.Location.End.Column != 12 {
		t.Errorf("edit End.Column = %d, want 12", edit.Location.End.Column)
	}
}

func TestNoUngracefulStopsignalRule_ViolationLocation(t *testing.T) {
	t.Parallel()

	content := "FROM alpine:3.20\nRUN echo hello\nSTOPSIGNAL SIGKILL\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewNoUngracefulStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Location.Start.Line != 3 {
		t.Errorf("Start.Line = %d, want 3", violations[0].Location.Start.Line)
	}
}

func TestNormalizeSignalName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"SIGKILL", "SIGKILL"},
		{"SIGSTOP", "SIGSTOP"},
		{"SIGTERM", "SIGTERM"},
		{"KILL", "SIGKILL"},
		{"STOP", "SIGSTOP"},
		{"TERM", "SIGTERM"},
		{"sigkill", "SIGKILL"},
		{"sigterm", "SIGTERM"},
		{"9", "SIGKILL"},
		{"19", "SIGSTOP"},
		{"15", "15"}, // Unknown numeric — returned as-is
		{"2", "2"},   // Unknown numeric — returned as-is
		{"SIGRTMIN+3", "SIGRTMIN+3"},
		{"SIGQUIT", "SIGQUIT"},
		{"SIGINT", "SIGINT"},
		{"SIGWINCH", "SIGWINCH"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := normalizeSignalName(tt.input)
			if got != tt.want {
				t.Errorf("normalizeSignalName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSignalColumnRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line      string
		wantStart int
		wantEnd   int
	}{
		{"STOPSIGNAL SIGKILL", 11, 18},
		{"STOPSIGNAL 9", 11, 12},
		{"  STOPSIGNAL SIGSTOP", 13, 20}, // indented
		{"STOPSIGNAL  SIGKILL", 12, 19},  // double space
		{"stopsignal sigkill", 11, 18},   // lowercase
		{"RUN echo hello", -1, -1},       // not a STOPSIGNAL line
		{"STOPSIGNAL", -1, -1},           // no signal value
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()
			start, end := signalColumnRange(tt.line)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("signalColumnRange(%q) = (%d, %d), want (%d, %d)",
					tt.line, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
