package ruby

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferNetworkNoneInstallRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewPreferNetworkNoneInstallRule().Metadata()
	if meta.Code != PreferNetworkNoneInstallRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, PreferNetworkNoneInstallRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
	}
}

func TestPreferNetworkNoneInstallRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferNetworkNoneInstallRule(), []testutil.RuleTestCase{
		// --- Violation ---
		{
			Name: "bundle install with bind+cache mounts but no --network=none triggers",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
RUN --mount=type=bind,source=Gemfile,target=Gemfile \
    --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
    --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
    bundle install
`,
			WantViolations: 1,
		},
		// --- Compliance: --network=none present ---
		{
			Name: "bundle install with --network=none suppresses",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
RUN --network=none \
    --mount=type=bind,source=Gemfile,target=Gemfile \
    --mount=type=bind,source=Gemfile.lock,target=Gemfile.lock \
    --mount=type=cache,target=${BUNDLE_PATH}/cache,sharing=locked \
    bundle install --local
`,
			WantViolations: 0,
		},
		// --- Suppressions ---
		{
			Name: "no syntax pragma → suppress",
			Content: `FROM ruby:3.3-slim
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "bare bundle install (no bind/cache mounts) does NOT trigger (other rules cover those)",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim
COPY Gemfile Gemfile.lock ./
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "non-Ruby stage skipped",
			Content: `# syntax=docker/dockerfile:1
FROM debian:bookworm-slim
RUN bundle install
`,
			WantViolations: 0,
		},
		{
			Name: "dev stage skipped",
			Content: `# syntax=docker/dockerfile:1
FROM ruby:3.3-slim AS dev
RUN bundle install
`,
			WantViolations: 0,
		},
	})
}
