package ruby

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseGemfile_Basic(t *testing.T) {
	t.Parallel()

	gem := mustParseGemfile(t, "Gemfile.basic")

	if got, want := gem.RubyConstraint, "3.3.5"; got != want {
		t.Errorf("RubyConstraint = %q, want %q", got, want)
	}
	if got, want := gem.Sources, []string{"https://rubygems.org"}; !slices.Equal(got, want) {
		t.Errorf("Sources = %v, want %v", got, want)
	}
	if !gem.HasDevGroup {
		t.Errorf("HasDevGroup = false, want true (multi-symbol :development, :test counts)")
	}
	if !gem.HasTestGroup {
		t.Errorf("HasTestGroup = false, want true")
	}
	if len(gem.GitGems) != 0 {
		t.Errorf("GitGems = %v, want []", gem.GitGems)
	}
}

func TestParseGemfile_GitGems(t *testing.T) {
	t.Parallel()

	gem := mustParseGemfile(t, "Gemfile.with-git-gems")

	if got, want := gem.RubyConstraint, "3.4.0"; got != want {
		t.Errorf("RubyConstraint = %q, want %q", got, want)
	}
	wantSources := []string{
		"https://rubygems.org",
		"https://gems.example.com",
	}
	if !slices.Equal(gem.Sources, wantSources) {
		t.Errorf("Sources = %v, want %v", gem.Sources, wantSources)
	}
	wantGit := []string{"rails", "rspec", "old-style", "multi-line"}
	if !slices.Equal(gem.GitGems, wantGit) {
		t.Errorf("GitGems = %v, want %v", gem.GitGems, wantGit)
	}
	if gem.HasDevGroup {
		t.Errorf("HasDevGroup = true, want false")
	}
	if !gem.HasTestGroup {
		t.Errorf("HasTestGroup = false, want true")
	}
}

func TestParseGemfile_NoDevGroup(t *testing.T) {
	t.Parallel()

	gem := mustParseGemfile(t, "Gemfile.no-dev-group")

	if got, want := gem.Sources, []string{"https://rubygems.org"}; !slices.Equal(got, want) {
		t.Errorf("Sources = %v, want %v", got, want)
	}
	if gem.HasDevGroup {
		t.Errorf("HasDevGroup = true, want false")
	}
	if gem.HasTestGroup {
		t.Errorf("HasTestGroup = true, want false")
	}
	if gem.RubyConstraint != "" {
		t.Errorf("RubyConstraint = %q, want empty", gem.RubyConstraint)
	}
}

func TestParseGemfile_EmptyContentReturnsNil(t *testing.T) {
	t.Parallel()

	cases := map[string][]byte{
		"nil":           nil,
		"empty":         {},
		"whitespace":    []byte("   \n\n   \n"),
		"only_comments": []byte("# Header comment\n# Another comment\n"),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ParseGemfile(content); got != nil {
				t.Errorf("ParseGemfile(%q) = %+v, want nil", name, got)
			}
		})
	}
}

// regression: a Gemfile containing only a single tracked-feature-free entry
// (e.g. `gem "rails"`) should still yield a non-nil GemfileFacts so rules
// that just check Gemfile presence can do so without losing signal.
func TestParseGemfile_PresentButNoTrackedFeatures(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"single_gem":  `gem "rails"` + "\n",
		"only_module": "module Foo; end\n",
		"only_code":   "puts 'hello'\n",
		"only_word":   "ruby\n",
		"gemspec":     "gemspec\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := ParseGemfile([]byte(content))
			if got == nil {
				t.Errorf("ParseGemfile(%q) = nil, want non-nil zero-value GemfileFacts", content)
			}
		})
	}
}

func TestParseGemfile_CommentStripping(t *testing.T) {
	t.Parallel()

	const content = `source "https://rubygems.org"
# gem "ignored", git: "https://nope.example.com"
gem "rails", git: "https://example.com/rails.git"   # trailing comment
gem "puma" # noisy
gem "with#hash", "1.0" # the hash inside double quotes is preserved
`
	gem := ParseGemfile([]byte(content))
	if gem == nil {
		t.Fatal("ParseGemfile returned nil")
	}
	if !slices.Contains(gem.GitGems, "rails") {
		t.Errorf("GitGems missing 'rails': %v", gem.GitGems)
	}
	if slices.Contains(gem.GitGems, "ignored") {
		t.Errorf("GitGems unexpectedly contains 'ignored': %v", gem.GitGems)
	}
	if slices.Contains(gem.GitGems, "with#hash") {
		t.Errorf("GitGems unexpectedly contains 'with#hash' (no git option): %v", gem.GitGems)
	}
}

func TestParseGemfile_MultiLineGitOptions(t *testing.T) {
	t.Parallel()

	const content = `source "https://rubygems.org"

gem "split-options",
    "~> 1.0",
    git: "https://example.com/split-options.git"

gem "after-comment", # trailing comment
    github: "owner/after-comment"

gem "backslash-cont", \
    git: "https://example.com/backslash.git"

gem "no-continuation"
gem "regular-version", "~> 5.0"
`
	gem := ParseGemfile([]byte(content))
	if gem == nil {
		t.Fatal("ParseGemfile returned nil")
	}
	want := []string{"split-options", "after-comment", "backslash-cont"}
	if !slices.Equal(gem.GitGems, want) {
		t.Errorf("GitGems = %v, want %v", gem.GitGems, want)
	}
}

func TestParseGemfile_ParenthesizedGemCalls(t *testing.T) {
	t.Parallel()

	const content = `source "https://rubygems.org"

gem("rails", "~> 8.0")
gem('paren-git', git: "https://example.com/paren-git.git")
gem ( "spaced-paren", github: "owner/spaced-paren" )
gem "regular", git: "https://example.com/regular.git"
`
	gem := ParseGemfile([]byte(content))
	if gem == nil {
		t.Fatal("ParseGemfile returned nil")
	}
	wantGit := []string{"paren-git", "spaced-paren", "regular"}
	if !slices.Equal(gem.GitGems, wantGit) {
		t.Errorf("GitGems = %v, want %v", gem.GitGems, wantGit)
	}
}

func TestParseGemfile_GitBlockGems(t *testing.T) {
	t.Parallel()

	const content = `source "https://rubygems.org"

git "https://github.com/example/inside-block.git" do
  gem "block-a"
  gem "block-b", "~> 1.0"
end

git_source(:internal) do |repo|
  gem "git-source-a", "owner/repo"
end

# This gem is outside any git block and has no git: option.
gem "not-git", "~> 5.0"

# Inline git: option still works.
gem "inline-git", git: "https://example.com/inline.git"

group :test do
  gem "test-only"
end
`
	gem := ParseGemfile([]byte(content))
	if gem == nil {
		t.Fatal("ParseGemfile returned nil")
	}
	wantGit := []string{"block-a", "block-b", "git-source-a", "inline-git"}
	if !slices.Equal(gem.GitGems, wantGit) {
		t.Errorf("GitGems = %v, want %v", gem.GitGems, wantGit)
	}
	if !gem.HasTestGroup {
		t.Errorf("HasTestGroup = false, want true")
	}
}

func TestParseGroupSymbols(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want []string
	}{
		"single":           {":development", []string{"development"}},
		"comma":            {":development, :test", []string{"development", "test"}},
		"extra_whitespace": {"  :development ,   :test  ", []string{"development", "test"}},
		"no_colon":         {"development", []string{"development"}},
		"empty":            {"", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := parseGroupSymbols(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Errorf("parseGroupSymbols(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestHasGitOrGithubOption(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		`, git: "https://example.com/foo.git"`:      true,
		`, github: "rails/rails"`:                   true,
		`, :git => "https://example.com/foo"`:       true,
		`, :github => "rails/rails"`:                true,
		`, "~> 8.0"`:                                false,
		`, branch: "main"`:                          false,
		`, source: "https://gems.example.com"`:      false,
		``:                                          false,
		` # describes git in a comment`:             false,
		`, version: '1.0', git: "https://nope.com"`: true,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			if got := hasGitOrGithubOption(in); got != want {
				t.Errorf("hasGitOrGithubOption(%q) = %v, want %v", in, got, want)
			}
		})
	}
}

func mustParseGemfile(t *testing.T, name string) *GemfileFacts {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %q: %v", name, err)
	}
	gem := ParseGemfile(data)
	if gem == nil {
		t.Fatalf("ParseGemfile(%q) returned nil", name)
	}
	return gem
}
