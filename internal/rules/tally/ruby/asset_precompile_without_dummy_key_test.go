package ruby

import (
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/facts"
	rubyfacts "github.com/wharflab/tally/internal/facts/ruby"
	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestAssetPrecompileWithoutDummyKeyRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewAssetPrecompileWithoutDummyKeyRule().Metadata()
	if meta.Code != AssetPrecompileWithoutDummyKeyRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, AssetPrecompileWithoutDummyKeyRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
	}
	if meta.FixPriority != assetPrecompileFixPriority {
		t.Errorf("FixPriority = %d, want %d", meta.FixPriority, assetPrecompileFixPriority)
	}
	if !strings.HasPrefix(meta.DocURL, "https://tally.wharflab.com/rules/tally/ruby/") {
		t.Errorf("DocURL = %q, want tally/ruby/ prefix", meta.DocURL)
	}
}

func TestAssetPrecompileWithoutDummyKeyRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewAssetPrecompileWithoutDummyKeyRule(), []testutil.RuleTestCase{
		// --- Regression: inline assignment must be scoped to the precompile command ---
		{
			Name: "inline assignment on a chained earlier command does NOT suppress (POSIX scoping)",
			Content: `FROM ruby:3.3-slim
RUN SECRET_KEY_BASE_DUMMY=1 echo ok && bin/rails assets:precompile
`,
			WantViolations: 1,
		},
		{
			Name: "inline assignment after a separator before the precompile does NOT suppress",
			Content: `FROM ruby:3.3-slim
RUN echo first; SECRET_KEY_BASE_DUMMY=1 echo second && bin/rails assets:precompile
`,
			WantViolations: 1,
		},
		// --- Violations ---
		{
			Name: "rails assets:precompile triggers",
			Content: `FROM ruby:3.3-slim
RUN rails assets:precompile
`,
			WantViolations: 1,
		},
		{
			Name: "bin/rails assets:precompile triggers",
			Content: `FROM ruby:3.3-slim
RUN bin/rails assets:precompile
`,
			WantViolations: 1,
		},
		{
			Name: "bundle exec rake assets:precompile triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle exec rake assets:precompile
`,
			WantViolations: 1,
		},
		{
			Name: "rake assets:precompile triggers",
			Content: `FROM ruby:3.3-slim
RUN rake assets:precompile
`,
			WantViolations: 1,
		},
		{
			Name: "bundle exec rails assets:precompile triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle exec rails assets:precompile
`,
			WantViolations: 1,
		},
		{
			Name: "multi-step RUN with chained assets:precompile triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install && bin/rails assets:precompile
`,
			WantViolations: 1,
		},
		{
			Name: "non-final stage still fires when ruby base",
			Content: `FROM ruby:3.3-slim AS builder
RUN bin/rails assets:precompile

FROM ruby:3.3-slim
CMD ["bin/rails", "server"]
`,
			WantViolations: 1,
		},
		// --- Compliant: inline placeholder ---
		{
			Name: "inline SECRET_KEY_BASE_DUMMY=1 prefix suppresses",
			Content: `FROM ruby:3.3-slim
RUN SECRET_KEY_BASE_DUMMY=1 bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		{
			Name: "inline SECRET_KEY_BASE=1 (rails-accepted alternative) suppresses",
			Content: `FROM ruby:3.3-slim
RUN SECRET_KEY_BASE=1 bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		{
			Name: "exported SECRET_KEY_BASE_DUMMY=1 earlier in same RUN suppresses",
			Content: `FROM ruby:3.3-slim
RUN export SECRET_KEY_BASE_DUMMY=1 && bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		{
			Name: "quoted SECRET_KEY_BASE_DUMMY value suppresses",
			Content: `FROM ruby:3.3-slim
RUN SECRET_KEY_BASE_DUMMY="dummy-value" bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		// --- Compliant: stage-level ENV ---
		{
			Name: "stage-level ENV SECRET_KEY_BASE_DUMMY suppresses",
			Content: `FROM ruby:3.3-slim
ENV SECRET_KEY_BASE_DUMMY=1
RUN bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		{
			Name: "stage-level ENV SECRET_KEY_BASE=1 suppresses",
			Content: `FROM ruby:3.3-slim
ENV SECRET_KEY_BASE=1
RUN bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		// --- Compliant: BuildKit secret mount path ---
		{
			Name: "secret mount with env=RAILS_MASTER_KEY suppresses",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
RUN --mount=type=secret,id=rails_master_key,env=RAILS_MASTER_KEY \
    bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		{
			Name: "secret mount with shell read of /run/secrets suppresses",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
RUN --mount=type=secret,id=rails_master_key \
    RAILS_MASTER_KEY="$(cat /run/secrets/rails_master_key)" \
    bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		// --- Compliant: dev/test stages ---
		{
			Name: "dev stage skipped",
			Content: `FROM ruby:3.3-slim AS dev
RUN bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		{
			Name: "test stage skipped",
			Content: `FROM ruby:3.3-slim AS test
RUN bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		// --- Guardrails: non-Ruby stage ignored ---
		{
			Name: "non-ruby base does not fire",
			Content: `FROM debian:stable-slim
RUN bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		{
			Name: "windows base does not fire",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN bin/rails assets:precompile
`,
			WantViolations: 0,
		},
		// --- False trigger guards ---
		{
			Name: "rake without assets:precompile does not fire",
			Content: `FROM ruby:3.3-slim
RUN bundle exec rake db:migrate
`,
			WantViolations: 0,
		},
		{
			Name: "rails without assets:precompile does not fire",
			Content: `FROM ruby:3.3-slim
RUN bin/rails routes
`,
			WantViolations: 0,
		},
		{
			Name: "bundle install alone does not fire",
			Content: `FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 0,
		},
	})
}

func TestAssetPrecompileWithoutDummyKey_Fix_PrependsInline(t *testing.T) {
	t.Parallel()

	src := `FROM ruby:3.3-slim
RUN bin/rails assets:precompile
`
	input := testutil.MakeLintInput(t, "Dockerfile", src)
	violations := NewAssetPrecompileWithoutDummyKeyRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	v := violations[0]
	if v.SuggestedFix == nil {
		t.Fatalf("expected a suggested fix, got nil")
	}
	if v.SuggestedFix.Safety != rules.FixSafe {
		t.Errorf("Safety = %v, want FixSafe", v.SuggestedFix.Safety)
	}
	got := string(fix.ApplyFix([]byte(src), v.SuggestedFix))
	if !strings.Contains(got, "RUN SECRET_KEY_BASE_DUMMY=1 bin/rails assets:precompile") {
		t.Errorf("fixed source missing inline prefix:\n%s", got)
	}
}

func TestAssetPrecompileWithoutDummyKey_Fix_RakePrefix(t *testing.T) {
	t.Parallel()

	src := `FROM ruby:3.3-slim
RUN bundle install && bundle exec rake assets:precompile
`
	input := testutil.MakeLintInput(t, "Dockerfile", src)
	violations := NewAssetPrecompileWithoutDummyKeyRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	got := string(fix.ApplyFix([]byte(src), violations[0].SuggestedFix))
	// The prefix must be applied to the asset-compile sub-command, not at the
	// front of the whole RUN, so unrelated bundle install isn't affected.
	wantSub := "&& SECRET_KEY_BASE_DUMMY=1 bundle exec rake assets:precompile"
	if !strings.Contains(got, wantSub) {
		t.Errorf("fixed source missing rake prefix; got:\n%s", got)
	}
}

func TestAssetPrecompileWithoutDummyKey_ContextDemotesSeverity(t *testing.T) {
	t.Parallel()

	// Build context observable but no encrypted credentials file: severity
	// should drop to info and detail should explain the demotion.
	reader := &fakeContextReader{
		files: map[string][]byte{
			"Gemfile.lock": []byte(`GEM
  remote: https://rubygems.org/
  specs:
    rails (8.0.0)

DEPENDENCIES
  rails

BUNDLED WITH
   2.5.6
`),
		},
	}
	src := `FROM ruby:3.3-slim
RUN bin/rails assets:precompile
`
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", src, reader)
	violations := NewAssetPrecompileWithoutDummyKeyRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].Severity != rules.SeverityInfo {
		t.Errorf("Severity = %v, want Info (no credentials.yml.enc observable)", violations[0].Severity)
	}
	if !strings.Contains(violations[0].Detail, "encrypted-credentials") {
		t.Errorf("Detail missing credentials note:\n%s", violations[0].Detail)
	}
}

func TestAssetPrecompileWithoutDummyKey_ContextWithCredentialsKeepsWarning(t *testing.T) {
	t.Parallel()

	reader := &fakeContextReader{
		files: map[string][]byte{
			"Gemfile.lock": []byte(`GEM
  specs:
    rails (8.0.0)

DEPENDENCIES
  rails

BUNDLED WITH
   2.5.6
`),
			"config/credentials.yml.enc": []byte("encrypted-blob"),
		},
	}
	src := `FROM ruby:3.3-slim
RUN bin/rails assets:precompile
`
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", src, reader)
	violations := NewAssetPrecompileWithoutDummyKeyRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].Severity != rules.SeverityWarning {
		t.Errorf("Severity = %v, want Warning", violations[0].Severity)
	}
}

func TestAssetPrecompileWithoutDummyKey_ContextOldRailsSwitchesToSecretMountSuggestion(t *testing.T) {
	t.Parallel()

	reader := &fakeContextReader{
		files: map[string][]byte{
			"Gemfile.lock": []byte(`GEM
  specs:
    rails (7.0.4)

DEPENDENCIES
  rails

BUNDLED WITH
   2.4.10
`),
		},
	}
	src := `FROM ruby:3.0-slim
RUN bin/rails assets:precompile
`
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", src, reader)
	violations := NewAssetPrecompileWithoutDummyKeyRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}
	if violations[0].SuggestedFix == nil {
		t.Fatalf("expected a suggested fix, got nil")
	}
	if violations[0].SuggestedFix.Safety != rules.FixSuggestion {
		t.Errorf("Safety = %v, want FixSuggestion (Rails < 7.1)", violations[0].SuggestedFix.Safety)
	}
	if !strings.Contains(violations[0].Detail, "Rails 7.1") {
		t.Errorf("Detail missing Rails 7.1 explanation:\n%s", violations[0].Detail)
	}
	// Edits should be empty for the structural suggestion.
	if len(violations[0].SuggestedFix.Edits) != 0 {
		t.Errorf("Edits = %d, want 0 for structural suggestion", len(violations[0].SuggestedFix.Edits))
	}
}

// --- helpers ---

// fakeContextReader is a minimal facts.ContextFileReader for tests.
type fakeContextReader struct {
	files   map[string][]byte
	ignored map[string]bool
}

var _ facts.ContextFileReader = (*fakeContextReader)(nil)
var _ rubyfacts.ContextFileReader = (*fakeContextReader)(nil)

func (r *fakeContextReader) FileExists(path string) bool {
	_, ok := r.files[path]
	return ok
}

func (r *fakeContextReader) ReadFile(path string) ([]byte, error) {
	if data, ok := r.files[path]; ok {
		return append([]byte(nil), data...), nil
	}
	return nil, fakeNotFoundError{}
}

func (r *fakeContextReader) IsIgnored(path string) (bool, error) {
	return r.ignored[path], nil
}

func (r *fakeContextReader) IsHeredocFile(string) bool { return false }

type fakeNotFoundError struct{}

func (fakeNotFoundError) Error() string { return "file not found" }
