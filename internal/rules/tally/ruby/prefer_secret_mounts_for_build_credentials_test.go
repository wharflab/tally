package ruby

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestPreferSecretMountsForBuildCredentialsRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewPreferSecretMountsForBuildCredentialsRule().Metadata()
	if meta.Code != PreferSecretMountsForBuildCredentialsRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, PreferSecretMountsForBuildCredentialsRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityInfo {
		t.Errorf("DefaultSeverity = %v, want Info", meta.DefaultSeverity)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
	}
}

func TestPreferSecretMountsForBuildCredentialsRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewPreferSecretMountsForBuildCredentialsRule(), []testutil.RuleTestCase{
		// --- Violations: well-known credential env vars ---
		{
			Name: "ENV BUNDLE_GITHUB__COM triggers",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_GITHUB__COM="user:token"
RUN bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "ARG BUNDLE_GITHUB__COM triggers",
			Content: `FROM ruby:3.3-slim
ARG BUNDLE_GITHUB__COM
RUN bundle install
`,
			WantViolations: 1,
		},
		{
			Name: "ENV GEM_HOST_API_KEY triggers",
			Content: `FROM ruby:3.3-slim
ENV GEM_HOST_API_KEY="abc123"
RUN gem push some.gem
`,
			WantViolations: 1,
		},
		{
			Name: "ENV NPM_TOKEN triggers (used during yarn install for asset compile)",
			Content: `FROM ruby:3.3-slim
ENV NPM_TOKEN="abc123"
RUN yarn install
`,
			WantViolations: 1,
		},
		// --- Violations: BUNDLE_<HOST>__<TLD> generic pattern ---
		{
			Name: "ARG BUNDLE_GEMS__MYCOMPANY__COM triggers (generic Bundler host pattern)",
			Content: `FROM ruby:3.3-slim
ARG BUNDLE_GEMS__MYCOMPANY__COM
`,
			WantViolations: 1,
		},
		// --- meta-ARG (before any FROM) ---
		{
			Name: "meta-ARG BUNDLE_GITHUB__COM triggers",
			Content: `ARG BUNDLE_GITHUB__COM
FROM ruby:3.3-slim
`,
			WantViolations: 1,
		},
		// --- Suppressions ---
		{
			Name: "non-credential ENV (BUNDLE_PATH) does not trigger (no host pattern)",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_PATH="/usr/local/bundle"
`,
			WantViolations: 0,
		},
		{
			Name: "non-Bundler ENV does not trigger",
			Content: `FROM ruby:3.3-slim
ENV RAILS_ENV="production"
`,
			WantViolations: 0,
		},
		{
			Name: "dev stage skipped",
			Content: `FROM ruby:3.3-slim AS dev
ENV BUNDLE_GITHUB__COM="user:token"
`,
			WantViolations: 0,
		},
		// --- Ruby gate: non-Ruby Dockerfiles should not trigger ---
		{
			Name: "Node.js stage with NPM_TOKEN does not trigger (not a Ruby rule's concern)",
			Content: `FROM node:20-slim
ENV NPM_TOKEN="abc"
RUN npm ci
`,
			WantViolations: 0,
		},
		{
			Name: "non-Ruby stage with YARN_AUTH_TOKEN does not trigger",
			Content: `FROM node:20-slim
ARG YARN_AUTH_TOKEN
RUN yarn install
`,
			WantViolations: 0,
		},
		{
			Name: "meta-ARG without any Ruby stage does not trigger",
			Content: `ARG BUNDLE_GITHUB__COM
FROM node:20-slim
`,
			WantViolations: 0,
		},
		// --- Multiple ARG vars on a single line ---
		{
			Name: "ARG with multiple credential vars yields one violation per var",
			Content: `FROM ruby:3.3-slim
ARG BUNDLE_GITHUB__COM BUNDLE_GITLAB__COM
RUN bundle install
`,
			WantViolations: 2,
		},
		// --- Bundler config-key disambiguation (codex P2) ---
		{
			Name: "BUNDLE_LOCAL__RACK is a config key (local.rack), not a credential",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_LOCAL__RACK=/path/to/rack
`,
			WantViolations: 0,
		},
		{
			Name: "BUNDLE_GEMFURY__IO triggers (.io TLD)",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_GEMFURY__IO="user:token"
`,
			WantViolations: 1,
		},
		{
			Name: "BUNDLE_BUILD__NOKOGIRI is per-gem build flag, not a credential",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_BUILD__NOKOGIRI=--use-system-libraries
`,
			WantViolations: 0,
		},
		{
			Name: "BUNDLE_GEMS__ACME__CO__UK triggers (compound TLD)",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_GEMS__ACME__CO__UK="user:token"
`,
			WantViolations: 1,
		},
		{
			Name: "BUNDLE_PRIVATE__HOST__DE triggers (German TLD)",
			Content: `FROM ruby:3.3-slim
ENV BUNDLE_PRIVATE__HOST__DE="user:token"
`,
			WantViolations: 1,
		},
		// --- Meta-ARG suppression honors per-stage filters (codex P2) ---
		{
			Name: "meta-ARG with only dev Ruby stage does not trigger",
			Content: `ARG BUNDLE_GITHUB__COM
FROM ruby:3.3-slim AS dev
`,
			WantViolations: 0,
		},
	})
}
