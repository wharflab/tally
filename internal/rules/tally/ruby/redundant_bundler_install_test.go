package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestRedundantBundlerInstallRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewRedundantBundlerInstallRule().Metadata()
	if meta.Code != RedundantBundlerInstallRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, RedundantBundlerInstallRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "performance" {
		t.Errorf("Category = %q, want %q", meta.Category, "performance")
	}
	if !strings.HasPrefix(meta.DocURL, "https://tally.wharflab.com/rules/tally/ruby/") {
		t.Errorf("DocURL = %q, want tally/ruby/ prefix", meta.DocURL)
	}
}

func TestRedundantBundlerInstallRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewRedundantBundlerInstallRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "gem install bundler on official ruby base triggers",
			Content: `FROM ruby:3.3-slim
RUN gem install bundler
`,
			WantViolations: 1,
		},
		{
			Name: "gem install bundler -v <version> triggers",
			Content: `FROM ruby:3.3-slim
RUN gem install bundler -v 2.5.6
`,
			WantViolations: 1,
		},
		{
			Name: "gem install bundler --version=X triggers",
			Content: `FROM ruby:3.3-slim
RUN gem install bundler --version=2.5.6
`,
			WantViolations: 1,
		},
		{
			Name: "gem install bundler in middle of chained command triggers",
			Content: `FROM ruby:3.3-slim
RUN apt-get update && gem install bundler && bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "gem install -v X bundler (flag-first form) triggers",
			Content: `FROM ruby:3.3-slim
RUN gem install -v 2.5.6 bundler
`,
			WantViolations: 1,
		},
		// --- Suppressions ---
		{
			Name: "non-official base (debian) does NOT trigger",
			Content: `FROM debian:bookworm-slim
RUN apt-get install -y ruby ruby-dev && gem install bundler
`,
			WantViolations: 0,
		},
		{
			Name: "alpine base (not from official ruby:*) does NOT trigger",
			Content: `FROM alpine:3.20
RUN apk add ruby ruby-dev && gem install bundler
`,
			WantViolations: 0,
		},
		{
			Name: "gem install bundler on jruby (Ruby derivative, but not official) does NOT trigger",
			Content: `FROM jruby:9.4
RUN gem install bundler
`,
			WantViolations: 0,
		},
		{
			Name: "gem install of a different gem (not bundler) does NOT trigger",
			Content: `FROM ruby:3.3-slim
RUN gem install rails
`,
			WantViolations: 0,
		},
		{
			Name: "dev stage skipped",
			Content: `FROM ruby:3.3-slim AS dev
RUN gem install bundler
`,
			WantViolations: 0,
		},
		// --- Multi-stage propagation ---
		{
			Name: "stage based on a ruby:* stage still triggers",
			Content: `FROM ruby:3.3-slim AS builder
RUN gem install bundler

FROM builder
RUN gem install bundler
`,
			WantViolations: 2,
		},
	})
}

func TestRedundantBundlerInstallRule_FixSuggestionDeletesWholeRunWhenSingleCommand(t *testing.T) {
	t.Parallel()

	content := `FROM ruby:3.3-slim
RUN gem install bundler
CMD ["bin/rails", "server"]
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewRedundantBundlerInstallRule().Check(input)
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
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("expected 1 edit; got %d", len(v.SuggestedFix.Edits))
	}
	if v.SuggestedFix.Edits[0].NewText != "" {
		t.Errorf("expected empty NewText (delete edit); got %q", v.SuggestedFix.Edits[0].NewText)
	}
}

func TestRedundantBundlerInstallRule_NoEditFixForChainedCommand(t *testing.T) {
	t.Parallel()

	// Multi-command RUN chains are too risky to auto-rewrite, so the fix
	// is a non-edit suggestion: a description explaining what to do, with
	// no Edits attached.
	content := `FROM ruby:3.3-slim
RUN gem install bundler && bundle install
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewRedundantBundlerInstallRule().Check(input)
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
	if len(v.SuggestedFix.Edits) != 0 {
		t.Errorf("expected no edits for chained command; got %d", len(v.SuggestedFix.Edits))
	}
}

func TestRedundantBundlerInstallRule_NoEditFixForMultiGemInstall(t *testing.T) {
	t.Parallel()

	// Regression for codex/gemini/greptile P1: `gem install bundler rails`
	// is a SINGLE CommandInfo, but auto-deleting that RUN would silently
	// remove the `rails` install too. The fix MUST fall back to a no-edit
	// suggestion so the user explicitly chooses how to disentangle the
	// chain.
	content := `FROM ruby:3.3-slim
RUN gem install bundler rails
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewRedundantBundlerInstallRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a suggested fix")
	}
	if len(v.SuggestedFix.Edits) != 0 {
		t.Errorf("multi-gem install must produce a no-edit suggestion; got %d edits",
			len(v.SuggestedFix.Edits))
	}
}

func TestRedundantBundlerInstallRule_DeleteFixOKForBundlerWithVersion(t *testing.T) {
	t.Parallel()

	// `gem install bundler -v 2.5.6` (bundler-only with a version pin)
	// SHOULD still get the whole-RUN delete fix — the `-v` flag and its
	// value don't add another gem target.
	content := `FROM ruby:3.3-slim
RUN gem install bundler -v 2.5.6
`
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewRedundantBundlerInstallRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatal("expected a suggested fix")
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Errorf("bundler-only install with -v should produce a delete edit; got %d edits",
			len(v.SuggestedFix.Edits))
	}
}
