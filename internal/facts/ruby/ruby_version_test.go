package ruby

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRubyVersionFile(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want string
	}{
		"plain":               {"3.3.5\n", "3.3.5"},
		"plain_no_newline":    {"3.3.5", "3.3.5"},
		"with_ruby_prefix":    {"ruby-3.3.5\n", "3.3.5"},
		"trailing_whitespace": {"  3.3.5  \n", "3.3.5"},
		"comments_skipped":    {"# header\n3.3.5\n", "3.3.5"},
		"empty":               {"", ""},
		"only_blank":          {"\n\n", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ParseRubyVersionFile([]byte(tc.in)); got != tc.want {
				t.Errorf("ParseRubyVersionFile(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseRubyVersionFile_Testdata(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "ruby-version.basic"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if got, want := ParseRubyVersionFile(data), "3.3.5"; got != want {
		t.Errorf("ParseRubyVersionFile = %q, want %q", got, want)
	}
}

func TestParseToolVersionsFile(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want string
	}{
		"plain":               {"ruby 3.3.5\n", "3.3.5"},
		"with_node":           {"nodejs 22.5.1\nruby 3.3.5\n", "3.3.5"},
		"comments_skipped":    {"# pinned\nruby 3.3.5\n", "3.3.5"},
		"multiple_versions":   {"ruby 3.3.5 3.4.0\n", "3.3.5"},
		"missing_ruby_line":   {"nodejs 22.5.1\n", ""},
		"empty":               {"", ""},
		"version_only_line":   {"3.3.5\n", ""},
		"case_insensitive":    {"Ruby 3.3.5\n", "3.3.5"},
		"trailing_whitespace": {"ruby 3.3.5   \n", "3.3.5"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ParseToolVersionsFile([]byte(tc.in)); got != tc.want {
				t.Errorf("ParseToolVersionsFile(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseToolVersionsFile_Testdata(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "tool-versions.basic"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if got, want := ParseToolVersionsFile(data), "3.3.5"; got != want {
		t.Errorf("ParseToolVersionsFile = %q, want %q", got, want)
	}
}
