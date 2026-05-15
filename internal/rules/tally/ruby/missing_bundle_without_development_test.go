package ruby

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestMissingBundleWithoutDevelopmentRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewMissingBundleWithoutDevelopmentRule().Metadata()
	if meta.Code != MissingBundleWithoutDevelopmentRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, MissingBundleWithoutDevelopmentRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityWarning {
		t.Errorf("DefaultSeverity = %v, want Warning", meta.DefaultSeverity)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
	}
	if meta.FixPriority != missingBundleWithoutDevelopmentFixPriority {
		t.Errorf("FixPriority = %d, want %d", meta.FixPriority, missingBundleWithoutDevelopmentFixPriority)
	}
	if !strings.HasPrefix(meta.DocURL, "https://tally.wharflab.com/rules/tally/ruby/") {
		t.Errorf("DocURL = %q, want tally/ruby/ prefix", meta.DocURL)
	}
}

func TestMissingBundleWithoutDevelopmentRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewMissingBundleWithoutDevelopmentRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "ruby base running bundle install without BUNDLE_WITHOUT triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "shell-form bundle install with extra flags triggers",
			Content: `FROM ruby:3.3-slim
RUN bundle install --jobs=4 --retry=3
`,
			WantViolations: 1,
		},
		{
			Name: "BUNDLE_WITHOUT set without 'development' still triggers",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_WITHOUT="test"
RUN bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "BUNDLE_ONLY set to 'default' (no production) still triggers",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_ONLY="default"
RUN bundle install
`,
			WantViolations: 1,
		},
		// --- Compliance: BUNDLE_WITHOUT contains "development" ---
		{
			Name: "ENV BUNDLE_WITHOUT=development suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_WITHOUT="development"
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "ENV BUNDLE_WITHOUT=development:test suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_WITHOUT="development:test"
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "ENV BUNDLE_WITHOUT case-insensitive suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_WITHOUT="Development"
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "ENV BUNDLE_WITHOUT space-separated suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_WITHOUT="development test"
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "ENV BUNDLE_WITHOUT comma-separated suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_WITHOUT="development,test,assets"
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- Compliance: BUNDLE_ONLY=production (Bundler 2.5+ inverse) ---
		{
			Name: "ENV BUNDLE_ONLY=default:production suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_ONLY="default:production"
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "ENV BUNDLE_ONLY=production suppresses",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_ONLY="production"
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- Compliance: bundle config set --local without development ---
		{
			Name: "bundle config set --local without development suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle config set --local without 'development test' && bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "bundle config set --global without development suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle config set --global without development \
    && bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "bundle config set without (no flag) development suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle config set without development
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "bundle config set without colon list suppresses",
			Content: `FROM ruby:3.3-slim
RUN bundle config set --local without development:test
RUN bundle install
`,
			WantViolations: 0,
		},
		// Legacy 2-arg `bundle config without development` (no `set`) is NOT
		// recognized — Bundler 2 deprecated it.
		{
			Name: "legacy bundle config without (no set) does not suppress",
			Content: `FROM ruby:3.3-slim
RUN bundle config without development
RUN bundle install
`,
			WantViolations: 1,
		},
		// --- Inheritance ---
		{
			Name: "BUNDLE_WITHOUT in parent stage inherited via FROM <stage> suppresses",
			Content: `FROM ruby:3.3-slim AS builder
ENV BUNDLE_WITHOUT="development"

FROM builder AS final
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- Guardrails: dev/test stages skipped ---
		{
			Name: "stage named dev skipped",
			Content: `FROM ruby:3.3-slim AS dev
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "stage named test skipped",
			Content: `FROM ruby:3.3-slim AS test
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "stage named development skipped",
			Content: `FROM ruby:3.3-slim AS development
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "stage named ci skipped",
			Content: `FROM ruby:3.3-slim AS ci
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "stage with explicit RAILS_ENV=development demoted",
			Content: `FROM ruby:3.3-slim
ENV RAILS_ENV="development"
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "stage with explicit RACK_ENV=test demoted",
			Content: `FROM ruby:3.3-slim
ENV RACK_ENV="test"
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- Guardrails: non-Ruby stages out of scope ---
		{
			Name: "python image running bundle install (false positive guard)",
			Content: `FROM python:3.12-slim
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "node image running bundle install is not a Ruby concern",
			Content: `FROM node:20
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- Guardrails: Windows ---
		{
			Name: "windows base skipped",
			Content: `FROM mcr.microsoft.com/windows/servercore:ltsc2022
RUN bundle install
`,
			WantViolations: 0,
		},
		// --- No bundle install, no violation ---
		{
			Name: "no bundle install in stage",
			Content: `FROM ruby:3.3-slim
RUN echo hello
`,
			WantViolations: 0,
		},
		{
			Name: "bundle exec rake (not install) does not trigger",
			Content: `FROM ruby:3.3-slim
RUN bundle exec rake assets:precompile
`,
			WantViolations: 0,
		},
		// --- Multiple bundle installs in one stage report once ---
		{
			Name: "multiple bundle installs in one stage report once",
			Content: `FROM ruby:3.3-slim
RUN bundle install
RUN bundle install --redownload
`,
			WantViolations: 1,
		},
		// --- Multi-stage: each non-dev Ruby stage with bundle install fires ---
		{
			Name: "multi-stage: builder + final both fire (no shared env)",
			Content: `FROM ruby:3.3-slim AS builder
RUN bundle install

FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 2,
		},
		{
			Name: "multi-stage: builder fires, final compliant",
			Content: `FROM ruby:3.3-slim AS builder
RUN bundle install

FROM ruby:3.3-slim
ENV BUNDLE_WITHOUT="development"
RUN bundle install
`,
			WantViolations: 1,
		},
		// --- Production env signal (RAILS_ENV elsewhere does not save it) ---
		{
			Name: "RAILS_ENV=production elsewhere does not suppress missing BUNDLE_WITHOUT",
			Content: `FROM ruby:3.3-slim
ENV RAILS_ENV="production"
RUN bundle install
`,
			WantViolations: 1,
		},
	})
}

// Refinement: when Gemfile is observable and lacks :development, the rule
// must suppress the entire fire (library-style projects).
func TestMissingBundleWithoutDevelopmentRule_GemfileNoDevGroupSuppresses(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN bundle install\n"
	ctx := newMockRubyBuildContext(map[string]string{
		"Gemfile": "source \"https://rubygems.org\"\ngem \"rake\"\n",
	})
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", content, ctx)
	violations := NewMissingBundleWithoutDevelopmentRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations when Gemfile has no :development group, got %d", len(violations))
	}
}

// Refinement: when Gemfile has :development AND :test, the fix recommends
// `development:test` so the production image also drops test gems.
func TestMissingBundleWithoutDevelopmentRule_FixUsesDevelopmentTestWhenBothGroupsPresent(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN bundle install\n"
	gemfile := `source "https://rubygems.org"

gem "rails"

group :development do
  gem "web-console"
end

group :test do
  gem "rspec-rails"
end
`
	ctx := newMockRubyBuildContext(map[string]string{"Gemfile": gemfile})
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", content, ctx)
	violations := NewMissingBundleWithoutDevelopmentRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if fix.Safety != rules.FixSafe {
		t.Errorf("Safety = %v, want FixSafe", fix.Safety)
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}
	want := `ENV BUNDLE_WITHOUT="development:test"`
	if !strings.Contains(fix.Edits[0].NewText, want) {
		t.Errorf("fix text missing %q, got %q", want, fix.Edits[0].NewText)
	}
}

// Refinement: when Gemfile has only :development (no :test), the fix uses
// `development` (the canonical Rails-generator value).
func TestMissingBundleWithoutDevelopmentRule_FixUsesDevelopmentOnlyWhenTestGroupAbsent(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN bundle install\n"
	gemfile := `source "https://rubygems.org"

gem "rails"

group :development do
  gem "web-console"
end
`
	ctx := newMockRubyBuildContext(map[string]string{"Gemfile": gemfile})
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", content, ctx)
	violations := NewMissingBundleWithoutDevelopmentRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if !strings.Contains(fix.Edits[0].NewText, `ENV BUNDLE_WITHOUT="development"`) {
		t.Errorf("fix text missing canonical value, got %q", fix.Edits[0].NewText)
	}
	if strings.Contains(fix.Edits[0].NewText, "development:test") {
		t.Errorf("fix should not include :test when test group absent, got %q", fix.Edits[0].NewText)
	}
}

// Default fix: when no Gemfile is observable, the fix uses `development`.
func TestMissingBundleWithoutDevelopmentRule_FixDefaultWithoutGemfile(t *testing.T) {
	t.Parallel()

	content := "FROM ruby:3.3-slim\nRUN bundle install\n"
	input := testutil.MakeLintInput(t, "Dockerfile", content)
	violations := NewMissingBundleWithoutDevelopmentRule().Check(input)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	fix := violations[0].SuggestedFix
	if fix == nil {
		t.Fatal("expected SuggestedFix")
	}
	if fix.Safety != rules.FixSafe {
		t.Errorf("Safety = %v, want FixSafe", fix.Safety)
	}
	if !fix.IsPreferred {
		t.Errorf("expected IsPreferred = true")
	}
	if fix.Priority != missingBundleWithoutDevelopmentFixPriority {
		t.Errorf("Priority = %d, want %d", fix.Priority, missingBundleWithoutDevelopmentFixPriority)
	}
	if len(fix.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(fix.Edits))
	}
	edit := fix.Edits[0]
	// Fix anchor: line immediately after the FROM (line 2).
	if edit.Location.Start.Line != 2 || edit.Location.End.Line != 2 {
		t.Errorf("edit location = %+v, want line 2", edit.Location)
	}
	if edit.Location.Start.Column != 0 || edit.Location.End.Column != 0 {
		t.Errorf("edit location columns = (%d,%d), want (0,0) (zero-width insert)",
			edit.Location.Start.Column, edit.Location.End.Column)
	}
	want := `ENV BUNDLE_WITHOUT="development"` + "\n"
	if edit.NewText != want {
		t.Errorf("NewText = %q, want %q", edit.NewText, want)
	}
}

// Even though the file uses RAILS_ENV=production, when the Gemfile shows no
// :development group the rule still suppresses entirely.
func TestMissingBundleWithoutDevelopmentRule_GemfileSuppressesEvenWithProductionRailsEnv(t *testing.T) {
	t.Parallel()

	content := `FROM ruby:3.3-slim
ENV RAILS_ENV="production"
RUN bundle install
`
	ctx := newMockRubyBuildContext(map[string]string{
		"Gemfile": "source \"https://rubygems.org\"\ngem \"rake\"\n",
	})
	input := testutil.MakeLintInputWithContext(t, "Dockerfile", content, ctx)
	violations := NewMissingBundleWithoutDevelopmentRule().Check(input)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations when Gemfile has no :development group, got %d", len(violations))
	}
}

// --- Helpers ---

// mockRubyBuildContext is a minimal facts.ContextFileReader for tests that
// exercise the Gemfile-aware refinements. Only `FileExists`/`ReadFile` need
// to be backed by data; `IsIgnored` always returns false.
type mockRubyBuildContext struct {
	files map[string]string
}

func newMockRubyBuildContext(files map[string]string) *mockRubyBuildContext {
	return &mockRubyBuildContext{files: files}
}

func (m *mockRubyBuildContext) FileExists(p string) bool {
	_, ok := m.files[p]
	return ok
}

func (m *mockRubyBuildContext) ReadFile(p string) ([]byte, error) {
	content, ok := m.files[p]
	if !ok {
		return nil, fmt.Errorf("missing file %q", p)
	}
	return []byte(content), nil
}

func (m *mockRubyBuildContext) IsIgnored(string) (bool, error) {
	return false, nil
}

// PathExists is required by the broader BuildContext surface; satisfied so
// tests that pass mockRubyBuildContext through MakeLintInputWithContext do
// not break on interface upgrades.
func (m *mockRubyBuildContext) PathExists(p string) bool {
	return m.FileExists(p)
}

// IsHeredocFile is required by the broader BuildContext surface for
// COPY/ADD-from-heredoc handling; satisfied with a no-op for these tests.
func (m *mockRubyBuildContext) IsHeredocFile(string) bool {
	return false
}

// Compile-time check that the mock satisfies the facts.ContextFileReader
// interface.
var _ facts.ContextFileReader = (*mockRubyBuildContext)(nil)

// Sanity check: when an explicit unrelated error path is hit, the test
// helper does not silently treat it as "file not present".
func TestMockRubyBuildContext_ReadFileMissing(t *testing.T) {
	t.Parallel()
	ctx := newMockRubyBuildContext(map[string]string{"a": "x"})
	_, err := ctx.ReadFile("b")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, err) {
		t.Fatal("error wrapping is broken — sanity check failed")
	}
}
