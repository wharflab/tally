package tally

import (
	"testing"
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
