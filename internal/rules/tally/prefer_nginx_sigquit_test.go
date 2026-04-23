package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferNginxSigquitRule_Metadata(t *testing.T) {
	t.Parallel()
	r := NewPreferNginxSigquitRule()
	meta := r.Metadata()

	if meta.Code != PreferNginxSigquitRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, PreferNginxSigquitRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "best-practice" {
		t.Errorf("Category = %q, want best-practice", meta.Category)
	}
}

func TestPreferNginxSigquitRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferNginxSigquitRule(), []testutil.RuleTestCase{
		// --- Violations: wrong signal ---
		{
			Name:           "nginx with SIGTERM — violation",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL SIGTERM\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"STOPSIGNAL SIGTERM should be SIGQUIT"},
		},
		{
			Name:           "openresty with SIGTERM — violation",
			Content:        "FROM openresty/openresty:alpine\nSTOPSIGNAL SIGTERM\nCMD [\"openresty\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"should be SIGQUIT"},
		},
		{
			Name:           "nginx with SIGINT — violation",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL SIGINT\nENTRYPOINT [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"STOPSIGNAL SIGINT should be SIGQUIT"},
		},
		{
			Name:           "nginx with SIGKILL — violation",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL SIGKILL\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"STOPSIGNAL SIGKILL should be SIGQUIT"},
		},
		{
			Name:           "absolute /usr/sbin/nginx with wrong signal — violation",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL SIGTERM\nCMD [\"/usr/sbin/nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
		},

		// --- Violations: missing STOPSIGNAL ---
		{
			Name:           "nginx missing STOPSIGNAL — violation",
			Content:        "FROM nginx:1.27\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"missing STOPSIGNAL SIGQUIT"},
		},
		{
			Name:           "openresty missing STOPSIGNAL — violation",
			Content:        "FROM openresty/openresty:alpine\nCMD [\"openresty\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
			WantMessages:   []string{"missing STOPSIGNAL SIGQUIT"},
		},
		{
			Name:           "ENTRYPOINT nginx missing STOPSIGNAL — violation",
			Content:        "FROM nginx:1.27\nENTRYPOINT [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
		},
		{
			Name: "ENTRYPOINT nginx + CMD args missing STOPSIGNAL — violation",
			Content: "FROM nginx:1.27\n" +
				"ENTRYPOINT [\"nginx\"]\n" +
				"CMD [\"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
		},

		// --- No violations: correct signal ---
		{
			Name:           "SIGQUIT correct — no violation",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL SIGQUIT\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "SIGQUIT correct on openresty — no violation",
			Content:        "FROM openresty/openresty:alpine\nSTOPSIGNAL SIGQUIT\nCMD [\"openresty\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "numeric 3 normalizes to SIGQUIT — no violation",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL 3\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "QUIT normalizes to SIGQUIT — no violation",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL QUIT\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 0,
		},

		// --- No violations: shell form (PID 1 hidden) ---
		{
			Name:           "shell-form CMD — no violation",
			Content:        "FROM nginx:1.27\nCMD nginx -g 'daemon off;'\n",
			WantViolations: 0,
		},
		{
			Name:           "shell-form ENTRYPOINT — no violation",
			Content:        "FROM nginx:1.27\nENTRYPOINT nginx -g 'daemon off;'\n",
			WantViolations: 0,
		},
		{
			Name:           "sh -c wrapper — no violation (first token is sh, not nginx)",
			Content:        "FROM nginx:1.27\nCMD [\"sh\", \"-c\", \"nginx -g 'daemon off;'\"]\n",
			WantViolations: 0,
		},

		// --- No violations: non-nginx ---
		{
			Name:           "postgres — no violation",
			Content:        "FROM postgres:16\nSTOPSIGNAL SIGTERM\nCMD [\"postgres\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "systemd — no violation",
			Content:        "FROM fedora:40\nSTOPSIGNAL SIGTERM\nENTRYPOINT [\"/sbin/init\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "nginx-debug binary — no violation (name mismatch)",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL SIGTERM\nCMD [\"nginx-debug\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 0,
		},
		{
			Name:           "no ENTRYPOINT or CMD — no violation",
			Content:        "FROM nginx:1.27\nRUN echo hello\n",
			WantViolations: 0,
		},

		// --- Skipped: env var ---
		{
			Name:           "env var in STOPSIGNAL — skipped",
			Content:        "FROM nginx:1.27\nSTOPSIGNAL $MY_SIGNAL\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 0,
		},

		// --- Windows stage (skipped) ---
		{
			Name: "Windows stage with nginx — skipped",
			Content: "FROM mcr.microsoft.com/windows/servercore:ltsc2022\n" +
				"CMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 0,
		},

		// --- Multi-stage ---
		{
			Name: "multi-stage — violation only in nginx stage",
			Content: "FROM alpine:3.20 AS builder\n" +
				"RUN echo build\n" +
				"\n" +
				"FROM nginx:1.27\n" +
				"CMD [\"nginx\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 1,
		},
		{
			Name: "multi-stage — both wrong and missing",
			Content: "FROM nginx:1.27 AS first\n" +
				"STOPSIGNAL SIGTERM\n" +
				"CMD [\"nginx\", \"-g\", \"daemon off;\"]\n" +
				"\n" +
				"FROM openresty/openresty:alpine AS second\n" +
				"CMD [\"openresty\", \"-g\", \"daemon off;\"]\n",
			WantViolations: 2,
		},
	})
}

func TestPreferNginxSigquitRule_ReplacementFix(t *testing.T) {
	t.Parallel()

	content := "FROM nginx:1.27\nSTOPSIGNAL SIGTERM\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferNginxSigquitRule()
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
	if edit.NewText != "SIGQUIT" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "SIGQUIT")
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

func TestPreferNginxSigquitRule_InsertionFix(t *testing.T) {
	t.Parallel()

	content := "FROM nginx:1.27\nCMD [\"nginx\", \"-g\", \"daemon off;\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferNginxSigquitRule()
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
	wantText := "# [tally] SIGQUIT is the graceful shutdown signal for nginx / openresty\nSTOPSIGNAL SIGQUIT\n"
	if edit.NewText != wantText {
		t.Errorf("NewText = %q, want %q", edit.NewText, wantText)
	}
	// Zero-width insertion before line 2 (the CMD line).
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

func TestPreferNginxSigquitRule_InsertionFix_EntrypointPrecedence(t *testing.T) {
	t.Parallel()

	// ENTRYPOINT should anchor the insertion even when a preceding CMD exists.
	content := "FROM nginx:1.27\nCMD [\"-g\", \"daemon off;\"]\nENTRYPOINT [\"nginx\"]\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferNginxSigquitRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix")
	}

	edit := v.SuggestedFix.Edits[0]
	// Should insert before line 3 (the ENTRYPOINT), not line 2 (the CMD).
	if edit.Location.Start.Line != 3 {
		t.Errorf("edit Start.Line = %d, want 3 (ENTRYPOINT)", edit.Location.Start.Line)
	}
}
