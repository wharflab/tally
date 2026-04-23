package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/testutil"
)

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
		{"1", "SIGHUP"},
		{"2", "SIGINT"},
		{"3", "SIGQUIT"},
		{"15", "SIGTERM"},
		{"28", "SIGWINCH"},
		{"42", "42"}, // Unknown numeric — returned as-is
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
		{"STOPSIGNAL SIGKILL\r", 11, 18}, // CRLF — trailing \r stripped
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

func TestIsNginxOrOpenResty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		executable string
		want       bool
	}{
		{"nginx", true},
		{"openresty", true},
		{"/usr/sbin/nginx", true},
		{"/usr/local/nginx/sbin/nginx", true},
		{"/usr/local/openresty/nginx/sbin/nginx", true},
		{"/usr/local/openresty/bin/openresty", true},
		{"/sbin/init", false},
		{"systemd", false},
		{"postgres", false},
		{"php-fpm", false},
		{"nginx-debug", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.executable, func(t *testing.T) {
			t.Parallel()
			if got := isNginxOrOpenResty(tt.executable); got != tt.want {
				t.Errorf("isNginxOrOpenResty(%q) = %v, want %v", tt.executable, got, tt.want)
			}
		})
	}
}

func TestSignalEditLocation_CRLF(t *testing.T) {
	t.Parallel()

	// CRLF line endings: fix should still produce correct column range.
	content := "FROM alpine:3.20\r\nSTOPSIGNAL TERM\r\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	r := NewPreferCanonicalStopsignalRule()
	violations := r.Check(input)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a SuggestedFix for CRLF input")
	}

	edit := v.SuggestedFix.Edits[0]
	if edit.NewText != "SIGTERM" {
		t.Errorf("NewText = %q, want %q", edit.NewText, "SIGTERM")
	}
	// "STOPSIGNAL " = 11, "TERM" = 4 chars, \r must not be included
	if edit.Location.Start.Column != 11 {
		t.Errorf("Start.Column = %d, want 11", edit.Location.Start.Column)
	}
	if edit.Location.End.Column != 15 {
		t.Errorf("End.Column = %d, want 15 (should not include \\r)", edit.Location.End.Column)
	}
}
