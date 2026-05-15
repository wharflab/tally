package ruby

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferGemfileBindMountsRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewPreferGemfileBindMountsRule().Metadata()
	if meta.Code != PreferGemfileBindMountsRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, PreferGemfileBindMountsRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "performance" {
		t.Errorf("Category = %q, want %q", meta.Category, "performance")
	}
}

func TestPreferGemfileBindMountsRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferGemfileBindMountsRule(), []testutil.RuleTestCase{
		// --- Violations ---
		{
			Name: "COPY Gemfile Gemfile.lock + RUN bundle install triggers (with syntax pragma)",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
COPY Gemfile Gemfile.lock ./
RUN bundle install
`,
			WantViolations: 1,
		},
		// --- Suppressions ---
		{
			Name: "no syntax pragma → suppress (bind mounts unsupported)",
			Content: `FROM ruby:3.3-slim
COPY Gemfile Gemfile.lock ./
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "COPY . . (catch-all) does NOT trigger",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
COPY . .
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "COPY without bundle install does NOT trigger",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
COPY Gemfile Gemfile.lock ./
`,
			WantViolations: 0,
		},
		{
			Name: "non-Ruby stage skipped",
			Content: `# syntax=docker/dockerfile:1
FROM debian:bookworm-slim
COPY Gemfile Gemfile.lock ./
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "dev stage skipped",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim AS dev
COPY Gemfile Gemfile.lock ./
RUN bundle install
`,
			WantViolations: 0,
		},
	})
}
