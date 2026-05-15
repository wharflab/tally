package ruby

import (
	"testing"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/testutil"
)

func TestSecretsInArgOrEnvRule_Metadata(t *testing.T) {
	t.Parallel()

	meta := NewSecretsInArgOrEnvRule().Metadata()
	if meta.Code != SecretsInArgOrEnvRuleCode {
		t.Errorf("Code = %q, want %q", meta.Code, SecretsInArgOrEnvRuleCode)
	}
	if meta.DefaultSeverity != rules.SeverityError {
		t.Errorf("DefaultSeverity = %v, want Error", meta.DefaultSeverity)
	}
	if meta.Category != "security" {
		t.Errorf("Category = %q, want %q", meta.Category, "security")
	}
}

func TestSecretsInArgOrEnvRule_Check(t *testing.T) {
	t.Parallel()

	testutil.RunRuleTests(t, NewSecretsInArgOrEnvRule(), []testutil.RuleTestCase{
		// --- Violations: ENV with non-placeholder values ---
		{
			Name: "ENV SECRET_KEY_BASE with literal value triggers",
			Content: `FROM ruby:3.3-slim
ENV SECRET_KEY_BASE="abc123"
`,
			WantViolations: 1,
		},
		{
			Name: "ENV RAILS_MASTER_KEY triggers",
			Content: `FROM ruby:3.3-slim
ENV RAILS_MASTER_KEY="secret_key_value"
`,
			WantViolations: 1,
		},
		{
			Name: "ENV DEVISE_SECRET_KEY triggers",
			Content: `FROM ruby:3.3-slim
ENV DEVISE_SECRET_KEY="some-pepper"
`,
			WantViolations: 1,
		},
		// --- Violations: ARG with non-placeholder value ---
		{
			Name: "ARG SECRET_KEY_BASE=value triggers",
			Content: `FROM ruby:3.3-slim
ARG SECRET_KEY_BASE=abc123
`,
			WantViolations: 1,
		},
		// --- Violations: meta-ARG (before any FROM) ---
		{
			Name: "meta-ARG RAILS_MASTER_KEY triggers",
			Content: `ARG RAILS_MASTER_KEY=defabc
FROM ruby:3.3-slim
`,
			WantViolations: 1,
		},
		// --- Compliance: placeholder values ---
		{
			Name: "ENV SECRET_KEY_BASE=1 (Rails-accepted placeholder) suppresses",
			Content: `FROM ruby:3.3-slim
ENV SECRET_KEY_BASE=1
`,
			WantViolations: 0,
		},
		{
			Name: `ENV SECRET_KEY_BASE="dummy" (placeholder) suppresses`,
			Content: `FROM ruby:3.3-slim
ENV SECRET_KEY_BASE="dummy"
`,
			WantViolations: 0,
		},
		// --- Compliance: no value (empty default ARG is suspicious but
		// we still flag it because --build-arg substitution would leak)
		{
			Name: "ARG SECRET_KEY_BASE without default still triggers",
			Content: `FROM ruby:3.3-slim
ARG SECRET_KEY_BASE
`,
			WantViolations: 1,
		},
		// --- Suppressions ---
		{
			Name: "non-secret ENV (RAILS_ENV) does not trigger",
			Content: `FROM ruby:3.3-slim
ENV RAILS_ENV="production"
`,
			WantViolations: 0,
		},
		{
			Name: "dev stage skipped",
			Content: `FROM ruby:3.3-slim AS dev
ENV SECRET_KEY_BASE="abc123"
`,
			WantViolations: 0,
		},
	})
}
