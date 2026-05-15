package ruby

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestParseLockfile_Basic(t *testing.T) {
	t.Parallel()

	lock := mustParseTestdata(t, "gemfile.lock.basic")

	if got, want := lock.BundledWith, "2.5.6"; got != want {
		t.Errorf("BundledWith = %q, want %q", got, want)
	}
	if got, want := lock.RubyVersion, "ruby 3.3.5p100"; got != want {
		t.Errorf("RubyVersion = %q, want %q", got, want)
	}
	if got, want := lock.Platforms, []string{"ruby", "x86_64-linux"}; !slices.Equal(got, want) {
		t.Errorf("Platforms = %v, want %v", got, want)
	}
	if got, want := lock.Sources, []string{"https://rubygems.org/"}; !slices.Equal(got, want) {
		t.Errorf("Sources = %v, want %v", got, want)
	}
	if got, want := lock.Specs["rails"], "8.0.0"; got != want {
		t.Errorf("Specs[rails] = %q, want %q", got, want)
	}
	if got, want := lock.Specs["actionpack"], "8.0.0"; got != want {
		t.Errorf("Specs[actionpack] = %q, want %q", got, want)
	}
	if got, want := lock.DirectDeps["rails"], "~> 8.0"; got != want {
		t.Errorf("DirectDeps[rails] = %q, want %q", got, want)
	}
	if got, want := lock.DirectDeps["rake"], ""; got != want {
		t.Errorf("DirectDeps[rake] = %q, want %q", got, want)
	}
	if lock.HasGitGems {
		t.Errorf("HasGitGems = true, want false")
	}
	if lock.HasPathGems {
		t.Errorf("HasPathGems = true, want false")
	}
	if len(lock.NativeExtGems) != 0 {
		t.Errorf("NativeExtGems = %v, want []", lock.NativeExtGems)
	}
}

func TestParseLockfile_WithGitSource(t *testing.T) {
	t.Parallel()

	lock := mustParseTestdata(t, "gemfile.lock.with-git-source")

	if !lock.HasGitGems {
		t.Errorf("HasGitGems = false, want true")
	}
	if lock.HasPathGems {
		t.Errorf("HasPathGems = true, want false")
	}
	wantSources := []string{
		"https://github.com/rails/rails.git",
		"https://rubygems.org/",
	}
	if !slices.Equal(lock.Sources, wantSources) {
		t.Errorf("Sources = %v, want %v", lock.Sources, wantSources)
	}
	if got, want := lock.Specs["rails"], "8.1.0.alpha"; got != want {
		t.Errorf("Specs[rails] = %q, want %q", got, want)
	}
	// "rails!" in DEPENDENCIES should land as "rails" key.
	if _, ok := lock.DirectDeps["rails"]; !ok {
		t.Errorf("DirectDeps missing 'rails'")
	}
}

func TestParseLockfile_WithPrivateSource(t *testing.T) {
	t.Parallel()

	lock := mustParseTestdata(t, "gemfile.lock.with-private-source")

	wantSources := []string{
		"https://rubygems.org/",
		"https://gems.example.com/",
	}
	if !slices.Equal(lock.Sources, wantSources) {
		t.Errorf("Sources = %v, want %v", lock.Sources, wantSources)
	}
}

func TestParseLockfile_WithNativeExtensions(t *testing.T) {
	t.Parallel()

	lock := mustParseTestdata(t, "gemfile.lock.with-native-extensions")

	wantNative := []string{
		"bcrypt",
		"bigdecimal",
		"ffi",
		"grpc",
		"mysql2",
		"nio4r",
		"nokogiri",
		"pg",
		"puma",
		"sqlite3",
	}
	if !slices.Equal(lock.NativeExtGems, wantNative) {
		t.Errorf("NativeExtGems = %v, want %v", lock.NativeExtGems, wantNative)
	}
	// Platform suffix stripping: nokogiri (1.16.7-x86_64-linux) -> "1.16.7".
	if got, want := lock.Specs["nokogiri"], "1.16.7"; got != want {
		t.Errorf("Specs[nokogiri] = %q, want %q", got, want)
	}
	if got, want := lock.Specs["pg"], "1.5.7"; got != want {
		t.Errorf("Specs[pg] = %q, want %q", got, want)
	}
}

func TestParseLockfile_NoBundledWithBlock(t *testing.T) {
	t.Parallel()

	lock := mustParseTestdata(t, "gemfile.lock.no-bundled-with-block")

	if lock.BundledWith != "" {
		t.Errorf("BundledWith = %q, want empty", lock.BundledWith)
	}
	if got, want := lock.Specs["rails"], "5.2.3"; got != want {
		t.Errorf("Specs[rails] = %q, want %q", got, want)
	}
	if got, want := lock.DirectDeps["rails"], "~> 5.2"; got != want {
		t.Errorf("DirectDeps[rails] = %q, want %q", got, want)
	}
}

func TestParseLockfile_EmptyAndUnrecognized(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"nil":          nil,
		"empty":        {},
		"whitespace":   []byte("   \n\n   \n"),
		"unrelated":    []byte("hello world\nthis is not a lockfile\n"),
		"only_comment": []byte("# A note about something else\n"),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ParseLockfile(content); got != nil {
				t.Errorf("ParseLockfile(%q) = %+v, want nil", name, got)
			}
		})
	}
}

func TestParseLockfile_CRLFLineEndings(t *testing.T) {
	t.Parallel()

	const lockfile = "GEM\r\n" +
		"  remote: https://rubygems.org/\r\n" +
		"  specs:\r\n" +
		"    rake (13.2.1)\r\n" +
		"\r\n" +
		"DEPENDENCIES\r\n" +
		"  rake\r\n" +
		"\r\n" +
		"BUNDLED WITH\r\n" +
		"   2.5.6\r\n"

	lock := ParseLockfile([]byte(lockfile))
	if lock == nil {
		t.Fatal("ParseLockfile returned nil for CRLF input")
	}
	if got, want := lock.BundledWith, "2.5.6"; got != want {
		t.Errorf("BundledWith = %q, want %q", got, want)
	}
	if got, want := lock.Specs["rake"], "13.2.1"; got != want {
		t.Errorf("Specs[rake] = %q, want %q", got, want)
	}
}

func TestParseSpecLine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in       string
		wantName string
		wantVer  string
	}{
		"plain":          {"actionpack (8.0.0)", "actionpack", "8.0.0"},
		"platform":       {"nokogiri (1.13.6-x86_64-linux)", "nokogiri", "1.13.6"},
		"prerelease":     {"rails (8.1.0.alpha)", "rails", "8.1.0.alpha"},
		"missing_paren":  {"weird-line", "weird-line", ""},
		"empty":          {"", "", ""},
		"java_platform":  {"jruby-jars (9.4.7.0-java)", "jruby-jars", "9.4.7.0"},
		"complex_suffix": {"sqlite3 (2.0.4-aarch64-linux-gnu)", "sqlite3", "2.0.4"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotName, gotVer := parseSpecLine(tc.in)
			if gotName != tc.wantName || gotVer != tc.wantVer {
				t.Errorf("parseSpecLine(%q) = (%q, %q), want (%q, %q)",
					tc.in, gotName, gotVer, tc.wantName, tc.wantVer)
			}
		})
	}
}

func TestParseDependencyLine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in             string
		wantName       string
		wantConstraint string
	}{
		"with_constraint":      {"rails (~> 8.0)", "rails", "~> 8.0"},
		"pinned":               {"rails!", "rails", ""},
		"pinned_w_constraint":  {"rails (= 8.0.0)!", "rails", "= 8.0.0"},
		"plain":                {"rake", "rake", ""},
		"empty":                {"", "", ""},
		"comma_in_constraint":  {"rails (>= 7.1, < 9)", "rails", ">= 7.1, < 9"},
		"mismatched_paren":     {"rails (~> 8.0", "rails", ""},
		"trailing_whitespace":  {"  rake  ", "rake", ""},
		"trailing_bang_spaces": {"rails ! ", "rails", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotName, gotConstraint := parseDependencyLine(tc.in)
			if gotName != tc.wantName || gotConstraint != tc.wantConstraint {
				t.Errorf("parseDependencyLine(%q) = (%q, %q), want (%q, %q)",
					tc.in, gotName, gotConstraint, tc.wantName, tc.wantConstraint)
			}
		})
	}
}

func TestParseLockfile_NativeExtOrder(t *testing.T) {
	t.Parallel()

	const lockfile = `GEM
  remote: https://rubygems.org/
  specs:
    pg (1.5.0)
    nokogiri (1.16.0)
    rake (13.2.1)
    pg (1.5.0)

DEPENDENCIES
  pg
  nokogiri
  rake

BUNDLED WITH
   2.5.6
`
	lock := ParseLockfile([]byte(lockfile))
	if lock == nil {
		t.Fatal("ParseLockfile returned nil")
	}
	want := []string{"pg", "nokogiri"}
	if !slices.Equal(lock.NativeExtGems, want) {
		t.Errorf("NativeExtGems = %v, want %v", lock.NativeExtGems, want)
	}
}

func TestParseLockfile_PathBlock(t *testing.T) {
	t.Parallel()

	const lockfile = `PATH
  remote: ../local-gem
  specs:
    local-gem (0.1.0)

GEM
  remote: https://rubygems.org/
  specs:
    rake (13.2.1)

DEPENDENCIES
  local-gem!
  rake

BUNDLED WITH
   2.5.6
`
	lock := ParseLockfile([]byte(lockfile))
	if lock == nil {
		t.Fatal("ParseLockfile returned nil")
	}
	if !lock.HasPathGems {
		t.Errorf("HasPathGems = false, want true")
	}
	if got, ok := lock.Specs["local-gem"]; !ok || got != "0.1.0" {
		t.Errorf("Specs[local-gem] = %q (ok=%v), want 0.1.0", got, ok)
	}
}

// regression: ensure the parser does not capture remote: lines from the GIT
// block as the primary GEM source.
func TestParseLockfile_MultipleSourcesIndependent(t *testing.T) {
	t.Parallel()

	lock := mustParseTestdata(t, "gemfile.lock.with-git-source")
	wantOrder := []string{
		"https://github.com/rails/rails.git",
		"https://rubygems.org/",
	}
	if !reflect.DeepEqual(lock.Sources, wantOrder) {
		t.Errorf("Sources = %v, want %v", lock.Sources, wantOrder)
	}
}

func mustParseTestdata(t *testing.T, name string) *LockfileFacts {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %q: %v", name, err)
	}
	lock := ParseLockfile(data)
	if lock == nil {
		t.Fatalf("ParseLockfile(%q) returned nil", name)
	}
	return lock
}
