package ruby

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestEOLRubyVersionRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewEOLRubyVersionRule().Metadata()
	if meta.Code != EOLRubyVersionRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, EOLRubyVersionRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
	}
}

func TestEOLRubyVersionRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewEOLRubyVersionRule(), []testutil.RuleTestCase{
		// --- Violations: EOL Ruby branches ---
		{
			Name: "ruby:2.7-slim (EOL) triggers",
			Content: `FROM ruby:2.7-slim
RUN bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "ruby:3.0 (EOL) triggers",
			Content: `FROM ruby:3.0
RUN bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "ruby:3.1.6 (EOL with patch) triggers",
			Content: `FROM ruby:3.1.6
RUN bundle install
`,
			WantViolations: 1,
		},
		// --- Compliance: supported Ruby branches ---
		{
			Name: "ruby:3.3-slim (supported) does NOT trigger",
			Content: `FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "ruby:3.4 (supported) does NOT trigger",
			Content: `FROM ruby:3.4
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- Suppressions ---
		{
			Name: "non-Ruby base does NOT trigger",
			Content: `FROM debian:bookworm-slim
RUN apt-get install -y ruby
`,
			WantViolations: 0,
		},
		{
			Name: "Ruby derivative (jruby) does NOT trigger",
			Content: `FROM jruby:9.4
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "ruby:latest (no parseable major.minor) does NOT trigger",
			Content: `FROM ruby:latest
RUN bundle install
`,
			WantViolations: 0,
		},
	})
}

func TestEOLRubyVersionRule_FixRewritesBranch(t *testing.T) {
	t.Parallel()

	content := `FROM ruby:3.0-slim
RUN bundle install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEOLRubyVersionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a suggested fix")
	}
	if v.SuggestedFix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion", v.SuggestedFix.Safety)
	}
	got := v.SuggestedFix.Edits[0].NewText
	want := "ruby:" + supportedRubyBranches[0] + "-slim"
	if got != want {
		t.Errorf("fix NewText = %q, want %q", got, want)
	}
}

func TestEOLRubyVersionRule_FixDropsPatchLevel(t *testing.T) {
	t.Parallel()

	// Regression for codex P2 / greptile P1 / gemini medium: rewriting
	// `ruby:3.1.6` → `ruby:3.4.6` would carry the old branch's patch
	// number into the new branch and produce a tag that may not exist
	// on Docker Hub. The fix must drop the patch level.
	content := `FROM ruby:3.1.6
RUN bundle install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEOLRubyVersionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a suggested fix")
	}
	got := v.SuggestedFix.Edits[0].NewText
	want := "ruby:" + supportedRubyBranches[0]
	if got != want {
		t.Errorf("fix NewText = %q, want %q (must drop patch level)", got, want)
	}
}

func TestEOLRubyVersionRule_FixPreservesVariant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		from string
		want string
	}{
		{"FROM ruby:3.0-slim\n", "ruby:" + supportedRubyBranches[0] + "-slim"},
		{"FROM ruby:3.0-bookworm\n", "ruby:" + supportedRubyBranches[0] + "-bookworm"},
		{"FROM ruby:3.0-alpine3.19\n", "ruby:" + supportedRubyBranches[0] + "-alpine3.19"},
		{"FROM ruby:2.7.0-alpine\n", "ruby:" + supportedRubyBranches[0] + "-alpine"},
	}
	for _, tt := range tests {
		t.Run(tt.from, func(t *testing.T) {
			t.Parallel()
			input := testutil.MakeLintInput(t, "Dockerfile", tt.from)
			violations := NewEOLRubyVersionRule().Check(input)
			if len(violations) != 1 {
				t.Fatalf("got %d violations, want 1", len(violations))
			}
			got := violations[0].SuggestedFix.Edits[0].NewText
			if got != tt.want {
				t.Errorf("fix NewText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEOLRubyVersionRule_NoFixForArgTemplated(t *testing.T) {
	t.Parallel()

	content := `ARG RUBY_VERSION=2.7
FROM ruby:${RUBY_VERSION}-slim
RUN bundle install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewEOLRubyVersionRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	// ARG-templated FROM lines need user judgment about whether to bump
	// the ARG default vs rewriting the FROM, so we leave a violation
	// without an auto-fix attached.
	if v.SuggestedFix != nil && len(v.SuggestedFix.Edits) > 0 {
		t.Errorf("ARG-templated FROM should not auto-rewrite; got %d edits",
			len(v.SuggestedFix.Edits))
	}
}

func TestParseRubyImageBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		image    string
		want     string
		wantRuby bool
	}{
		{name: "ruby:3.3-slim", image: "ruby:3.3-slim", want: "3.3", wantRuby: true},
		{name: "ruby:2.7", image: "ruby:2.7", want: "2.7", wantRuby: true},
		{name: "ruby:3.0.6-bookworm", image: "ruby:3.0.6-bookworm", want: "3.0", wantRuby: true},
		{name: "ruby:latest", image: "ruby:latest", want: "", wantRuby: true},
		{name: "ruby (no tag)", image: "ruby", want: "", wantRuby: true},
		{name: "jruby:9.4", image: "jruby:9.4", want: "", wantRuby: false},
		{name: "debian:bookworm", image: "debian:bookworm", want: "", wantRuby: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotBranch, gotRuby := parseRubyImageBranch(tt.image)
			if gotBranch != tt.want {
				t.Errorf("branch = %q, want %q", gotBranch, tt.want)
			}
			if gotRuby != tt.wantRuby {
				t.Errorf("isRuby = %v, want %v", gotRuby, tt.wantRuby)
			}
		})
	}
}

func TestMajorMinorFromVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"3.3.5", "3.3"},
		{"3.3.5p100", "3.3"},
		{"2.7.0", "2.7"},
		{"  3.4  ", "3.4"}, // trims whitespace; only major.minor needed
		{"abc.def", ""},
		{"3", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := majorMinorFromVersion(tt.in)
		if got != tt.want {
			t.Errorf("majorMinorFromVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
