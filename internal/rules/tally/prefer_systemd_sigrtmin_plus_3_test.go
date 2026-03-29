package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferSystemdSigrtminPlus3Rule_Metadata(t *testing.T) {
	t.Parallel()
	r := NewPreferSystemdSigrtminPlus3Rule()
	meta := r.Metadata()

	if meta.Code != PreferSystemdSigrtminPlus3RuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, PreferSystemdSigrtminPlus3RuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want correctness", meta.Category)
	}
}

func TestPreferSystemdSigrtminPlus3Rule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferSystemdSigrtminPlus3Rule(), []testutil.RuleTestCase{
		// --- Violations: wrong signal ---
		{
			Name:           "/sbin/init with SIGTERM — violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL SIGTERM\nENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"STOPSIGNAL SIGTERM should be SIGRTMIN+3"},
		},
		{
			Name:           "/usr/sbin/init with SIGINT — violation",
			Content:        "FROM centos:stream9\nSTOPSIGNAL SIGINT\nENTRYPOINT [\"/usr/sbin/init\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"should be SIGRTMIN+3"},
		},
		{
			Name:           "/lib/systemd/systemd with SIGTERM — violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL SIGTERM\nENTRYPOINT [\"/lib/systemd/systemd\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"should be SIGRTMIN+3"},
		},
		{
			Name:           "/usr/lib/systemd/systemd with SIGTERM — violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL SIGTERM\nENTRYPOINT [\"/usr/lib/systemd/systemd\"]\n",
			WantViolations: 1,
		},
		{
			Name:           "bare systemd with SIGKILL — violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL SIGKILL\nCMD [\"systemd\", \"--system\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"STOPSIGNAL SIGKILL should be SIGRTMIN+3"},
		},
		{
			Name:           "/sbin/init via CMD with SIGTERM — violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL SIGTERM\nCMD [\"/sbin/init\"]\n",
			WantViolations: 1,
		},
		{
			Name:           "systemd with SIGRTMIN (without +3) — violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL SIGRTMIN\nENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"STOPSIGNAL SIGRTMIN should be SIGRTMIN+3"},
		},

		// --- Violations: missing STOPSIGNAL ---
		{
			Name:           "/sbin/init missing STOPSIGNAL — violation",
			Content:        "FROM fedora:40\nENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"missing STOPSIGNAL SIGRTMIN+3"},
		},
		{
			Name:           "/usr/lib/systemd/systemd CMD missing STOPSIGNAL — violation",
			Content:        "FROM centos:stream9\nCMD [\"/usr/lib/systemd/systemd\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"missing STOPSIGNAL SIGRTMIN+3"},
		},
		{
			Name: "systemd with args missing STOPSIGNAL — violation",
			Content: "FROM fedora:40\n" +
				"ENTRYPOINT [\"systemd\", \"--system\"]\n",
			WantViolations: 1,
		},

		// --- No violations: correct signal ---
		{
			Name:           "SIGRTMIN+3 correct — no violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL SIGRTMIN+3\nENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "RTMIN+3 normalizes to SIGRTMIN+3 — no violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL RTMIN+3\nENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 0,
		},

		// --- No violations: shell form (PID 1 hidden) ---
		{
			Name:           "shell-form ENTRYPOINT — no violation",
			Content:        "FROM fedora:40\nENTRYPOINT /sbin/init\n",
			WantViolations: 0,
		},
		{
			Name:           "shell-form CMD — no violation",
			Content:        "FROM fedora:40\nCMD /sbin/init\n",
			WantViolations: 0,
		},

		// --- No violations: non-systemd ---
		{
			Name:           "nginx — no violation",
			Content:        "FROM nginx:1.27\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "postgres — no violation",
			Content:        "FROM postgres:16\nCMD [\"postgres\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "no ENTRYPOINT or CMD — no violation",
			Content:        "FROM fedora:40\nRUN echo hello\n",
			WantViolations: 0,
		},

		// --- Skipped: env var ---
		{
			Name:           "env var in STOPSIGNAL — skipped",
			Content:        "FROM fedora:40\nSTOPSIGNAL $MY_SIGNAL\nENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 0,
		},

		// --- Windows stage (skipped) ---
		{
			Name: "Windows stage with /sbin/init — skipped",
			Content: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"ENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 0,
		},

		// --- Multi-stage ---
		{
			Name: "multi-stage — violation only in systemd stage",
			Content: "FROM alpine:3.20 AS builder\n" +
				"RUN echo build\n" +
				"\n" +
				"FROM fedora:40\n" +
				"ENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 1,
		},
		{
			Name: "multi-stage — both wrong and missing",
			Content: "FROM fedora:40 AS first\n" +
				"STOPSIGNAL SIGTERM\n" +
				"ENTRYPOINT [\"/sbin/init\"]\n" +
				"\n" +
				"FROM centos:stream9 AS second\n" +
				"CMD [\"/usr/sbin/init\"]\n",
			WantViolations: 2,
		},
		{
			Name: "multi-stage — systemd correct, other stages fine",
			Content: "FROM alpine:3.20 AS builder\n" +
				"RUN echo build\n" +
				"\n" +
				"FROM fedora:40\n" +
				"STOPSIGNAL SIGRTMIN+3\n" +
				"ENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 0,
		},
	})
}

func TestPreferSystemdSigrtminPlus3Rule_ReplacementFix(t *testing.T) {
	t.Parallel()

	content := "FROM fedora:40\nSTOPSIGNAL SIGTERM\nENTRYPOINT [\"/sbin/init\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferSystemdSigrtminPlus3Rule()
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
	if v.SuggestedFix.Priority != -1 {
		t.Errorf("Priority = %d, want -1", v.SuggestedFix.Priority)
	}
	if !v.SuggestedFix.IsPreferred {
		t.Error("expected IsPreferred = true")
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}

	edit := v.SuggestedFix.Edits[0]
	if edit.NewText != "SIGRTMIN+3" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "SIGRTMIN+3")
	}
	// "STOPSIGNAL " = 11 chars, "SIGTERM" starts at column 11, ends at 18
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit Start.Line = %d, want 2", edit.Location.Start.Line)
	}
	if edit.Location.Start.Column != 11 {
		t.Errorf("edit Start.Column = %d, want 11", edit.Location.Start.Column)
	}
	if edit.Location.End.Column != 18 {
		t.Errorf("edit End.Column = %d, want 18", edit.Location.End.Column)
	}
}

func TestPreferSystemdSigrtminPlus3Rule_InsertionFix(t *testing.T) {
	t.Parallel()

	content := "FROM fedora:40\nENTRYPOINT [\"/sbin/init\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferSystemdSigrtminPlus3Rule()
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
	if v.SuggestedFix.Priority != -1 {
		t.Errorf("Priority = %d, want -1", v.SuggestedFix.Priority)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(v.SuggestedFix.Edits))
	}

	edit := v.SuggestedFix.Edits[0]
	wantText := "# [tally] SIGRTMIN+3 is the graceful shutdown signal for systemd/init\nSTOPSIGNAL SIGRTMIN+3\n"
	if edit.NewText != wantText {
		t.Errorf("NewText = %q, want %q", edit.NewText, wantText)
	}
	// Zero-width insertion before line 2 (the ENTRYPOINT line)
	if edit.Location.Start.Line != 2 {
		t.Errorf("edit Start.Line = %d, want 2", edit.Location.Start.Line)
	}
	if edit.Location.Start.Column != 0 {
		t.Errorf("edit Start.Column = %d, want 0", edit.Location.Start.Column)
	}
	if edit.Location.End.Line != 2 {
		t.Errorf("edit End.Line = %d, want 2", edit.Location.End.Line)
	}
	if edit.Location.End.Column != 0 {
		t.Errorf("edit End.Column = %d, want 0", edit.Location.End.Column)
	}
}

func TestIsSystemdInit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		executable string
		want       bool
	}{
		{"/sbin/init", true},
		{"/usr/sbin/init", true},
		{"/lib/systemd/systemd", true},
		{"/usr/lib/systemd/systemd", true},
		{"systemd", true},
		{"nginx", false},
		{"/usr/bin/nginx", false},
		{"init", false},
		{"postgres", false},
		{"/usr/bin/systemd-resolved", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.executable, func(t *testing.T) {
			t.Parallel()
			if got := isSystemdInit(tt.executable); got != tt.want {
				t.Errorf("isSystemdInit(%q) = %v, want %v", tt.executable, got, tt.want)
			}
		})
	}
}

func TestStageRuntimeExecutable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "exec-form ENTRYPOINT",
			content: "FROM fedora:40\nENTRYPOINT [\"/sbin/init\"]\n",
			want:    "/sbin/init",
		},
		{
			name:    "exec-form CMD",
			content: "FROM fedora:40\nCMD [\"/sbin/init\"]\n",
			want:    "/sbin/init",
		},
		{
			name:    "exec-form ENTRYPOINT with args",
			content: "FROM fedora:40\nENTRYPOINT [\"/sbin/init\", \"--system\"]\n",
			want:    "/sbin/init",
		},
		{
			name:    "ENTRYPOINT takes precedence over CMD",
			content: "FROM fedora:40\nCMD [\"postgres\"]\nENTRYPOINT [\"/sbin/init\"]\n",
			want:    "/sbin/init",
		},
		{
			name:    "shell-form ENTRYPOINT returns empty",
			content: "FROM fedora:40\nENTRYPOINT /sbin/init\n",
			want:    "",
		},
		{
			name:    "shell-form CMD returns empty",
			content: "FROM fedora:40\nCMD /sbin/init\n",
			want:    "",
		},
		{
			name:    "no ENTRYPOINT or CMD returns empty",
			content: "FROM fedora:40\nRUN echo hello\n",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.content)
			if len(input.Stages) == 0 {
				t.Fatal("expected at least one stage")
			}
			got := stageRuntimeExecutable(input.Stages[0])
			if got != tt.want {
				t.Errorf("stageRuntimeExecutable() = %q, want %q", got, tt.want)
			}
		})
	}
}
