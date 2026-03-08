package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/shell"
)

func TestIsIPHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"192.168.1.1", true},
		{"10.0.0.1:8080", true},
		{"127.0.0.1:3000", true},
		{"[::1]", true},
		{"[::1]:8080", true},
		{"0.0.0.0", true},
		{"234.2.34.55:8666", true},
		{"example.com", false},
		{"example.com:443", false},
		{"go.dev", false},
		{"localhost", false},
		{"localhost:8080", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			t.Parallel()
			if got := isIPHost(tt.host); got != tt.want {
				t.Errorf("isIPHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestCurlTargetsOnlyIPs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  shell.CommandInfo
		want bool
	}{
		{
			name: "single IP URL",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"http://127.0.0.1/health"}},
			want: true,
		},
		{
			name: "IP URL with port",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"http://192.168.1.1:8080/api"}},
			want: true,
		},
		{
			name: "hostname URL",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"https://example.com/file"}},
			want: false,
		},
		{
			name: "mixed IP and hostname",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"http://127.0.0.1/a", "https://example.com/b"}},
			want: false,
		},
		{
			name: "no URLs",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"-fsSL", "-o", "/tmp/file"}},
			want: false,
		},
		{
			name: "multiple IP URLs",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"http://10.0.0.1/a", "http://10.0.0.2/b"}},
			want: true,
		},
		{
			name: "IP with flags",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"-s", "http://234.2.34.55:8666/data"}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := curlTargetsOnlyIPs(&tt.cmd); got != tt.want {
				t.Errorf("curlTargetsOnlyIPs(%v) = %v, want %v", tt.cmd.Args, got, tt.want)
			}
		})
	}
}

func TestCurlMissingLocationMetadata(t *testing.T) {
	t.Parallel()
	r := NewCurlMissingLocationRule()
	meta := r.Metadata()

	if meta.Code != "tally/curl-missing-location" {
		t.Errorf("Code = %q, want %q", meta.Code, "tally/curl-missing-location")
	}
	if meta.DefaultSeverity != 1 { // SeverityWarning
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want %q", meta.Category, "correctness")
	}
}
