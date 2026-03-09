package tally

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
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

func TestCurlIsNonTransfer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  shell.CommandInfo
		want bool
	}{
		{
			name: "curl --help",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"--help"}},
			want: true,
		},
		{
			name: "curl -h",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"-h"}},
			want: true,
		},
		{
			name: "curl --version",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"--version"}},
			want: true,
		},
		{
			name: "curl -V",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"-V"}},
			want: true,
		},
		{
			name: "curl --manual",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"--manual"}},
			want: true,
		},
		{
			name: "normal curl with URL",
			cmd:  shell.CommandInfo{Name: "curl", Args: []string{"-fsSL", "https://example.com"}},
			want: false,
		},
		{
			name: "curl with no args",
			cmd:  shell.CommandInfo{Name: "curl", Args: nil},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := curlIsNonTransfer(&tt.cmd); got != tt.want {
				t.Errorf("curlIsNonTransfer(%v) = %v, want %v", tt.cmd.Args, got, tt.want)
			}
		})
	}
}

func TestCurlNeedsFollow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cmd  shell.CommandInfo
		want bool
	}{
		{"no -X flag", shell.CommandInfo{Name: "curl", Args: []string{"-fsSL", "https://example.com"}}, false},
		{"-X GET", shell.CommandInfo{Name: "curl", Args: []string{"-X", "GET", "https://example.com"}}, false},
		{"-X POST", shell.CommandInfo{Name: "curl", Args: []string{"-X", "POST", "-d", "data", "https://example.com"}}, false},
		{"-X PUT", shell.CommandInfo{Name: "curl", Args: []string{"-X", "PUT", "-d", "data", "https://example.com"}}, false},
		{"-X DELETE", shell.CommandInfo{Name: "curl", Args: []string{"-X", "DELETE", "https://example.com/item"}}, true},
		{"-X PATCH", shell.CommandInfo{Name: "curl", Args: []string{"-X", "PATCH", "-d", "data", "https://example.com"}}, true},
		{"--request DELETE", shell.CommandInfo{Name: "curl", Args: []string{"--request", "DELETE", "https://example.com"}}, true},
		{"-X QUERY", shell.CommandInfo{Name: "curl", Args: []string{"-X", "QUERY", "-d", "q", "https://example.com"}}, true},
		{"-X get (lowercase)", shell.CommandInfo{Name: "curl", Args: []string{"-X", "get", "https://example.com"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := curlNeedsFollow(&tt.cmd); got != tt.want {
				t.Errorf("curlNeedsFollow(%v) = %v, want %v", tt.cmd.Args, got, tt.want)
			}
		})
	}
}

func TestCurlShouldFollowRedirectsMetadata(t *testing.T) {
	t.Parallel()
	r := NewCurlShouldFollowRedirectsRule()
	meta := r.Metadata()

	if meta.Code != "tally/curl-should-follow-redirects" {
		t.Errorf("Code = %q, want %q", meta.Code, "tally/curl-should-follow-redirects")
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "correctness" {
		t.Errorf("Category = %q, want %q", meta.Category, "correctness")
	}
}
