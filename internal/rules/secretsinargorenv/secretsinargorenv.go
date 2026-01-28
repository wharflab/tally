// Package secretsinargorenv implements the SecretsUsedInArgOrEnv rule.
// This rule warns when ARG or ENV variable names suggest they may contain secrets,
// which would be visible in image history and layers.
package secretsinargorenv

import (
	"regexp"
	"strings"
	"sync"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Rule implements the SecretsUsedInArgOrEnv linting rule.
// It detects ARG and ENV declarations with names that suggest secrets.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:             rules.BuildKitRulePrefix + "SecretsUsedInArgOrEnv",
		Name:             "Secrets in ARG or ENV",
		Description:      "Sensitive data should not be used in build-time variables",
		DocURL:           "https://docs.docker.com/go/dockerfile/rule/secrets-used-in-arg-or-env/",
		DefaultSeverity:  rules.SeverityWarning,
		Category:         "security",
		EnabledByDefault: true,
		IsExperimental:   false,
	}
}

// Check runs the SecretsUsedInArgOrEnv rule.
// It scans ARG and ENV instructions for variable names that suggest secrets.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation

	// Check global ARGs (meta args before first FROM)
	for _, arg := range input.MetaArgs {
		for _, kv := range arg.Args {
			if isSecretKey(kv.Key) {
				loc := rules.NewLocationFromRanges(input.File, arg.Location())
				violations = append(violations, r.createViolation(loc, "ARG", kv.Key))
			}
		}
	}

	// Check stage-level ARGs and ENVs
	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.ArgCommand:
				for _, kv := range c.Args {
					if isSecretKey(kv.Key) {
						loc := rules.NewLocationFromRanges(input.File, c.Location())
						violations = append(violations, r.createViolation(loc, "ARG", kv.Key))
					}
				}
			case *instructions.EnvCommand:
				for _, kv := range c.Env {
					if isSecretKey(kv.Key) {
						loc := rules.NewLocationFromRanges(input.File, c.Location())
						violations = append(violations, r.createViolation(loc, "ENV", kv.Key))
					}
				}
			}
		}
	}

	return violations
}

// createViolation creates a violation for a secret key.
func (r *Rule) createViolation(loc rules.Location, instruction, key string) rules.Violation {
	return rules.NewViolation(
		loc,
		r.Metadata().Code,
		"Do not use ARG or ENV instructions for sensitive data ("+key+")",
		r.Metadata().DefaultSeverity,
	).WithDocURL(r.Metadata().DocURL).WithDetail(
		"Using " + instruction + " " + key + " may leak secrets in image history. " +
			"Use --mount=type=secret instead for build-time secrets.")
}

// Secret detection patterns from BuildKit.
// Source: github.com/moby/buildkit/frontend/dockerfile/dockerfile2llb/convert.go
// Function: getSecretsRegex()
//
// Pattern matches secret-like tokens at word boundaries (start/end of string or underscore).
// Examples that match: API_KEY, DATABASE_PASSWORD, AUTH_TOKEN
// Examples that don't match: MONKEY (no word boundary before "key"), MYPASSWORDHERE
var (
	secretsRegexpOnce sync.Once
	secretsRegexp     *regexp.Regexp
	allowRegexp       *regexp.Regexp
)

// initSecretsRegexp compiles the secret detection regexps.
func initSecretsRegexp() {
	// Tokens that suggest a secret - must appear at word boundary
	secretTokens := []string{
		"apikey",
		"auth",
		"credential",
		"credentials",
		"key",
		"password",
		"pword",
		"passwd",
		"secret",
		"token",
	}
	pattern := `(?i)(?:_|^)(?:` + strings.Join(secretTokens, "|") + `)(?:_|$)`
	secretsRegexp = regexp.MustCompile(pattern)

	// Tokens that indicate NOT a secret (e.g., PUBLIC_KEY is fine)
	allowTokens := []string{
		"public",
	}
	allowPattern := `(?i)(?:_|^)(?:` + strings.Join(allowTokens, "|") + `)(?:_|$)`
	allowRegexp = regexp.MustCompile(allowPattern)
}

// isSecretKey checks if a variable name suggests it contains a secret.
// Uses the same logic as BuildKit's validateNoSecretKey.
func isSecretKey(key string) bool {
	secretsRegexpOnce.Do(initSecretsRegexp)
	return secretsRegexp.MatchString(key) && !allowRegexp.MatchString(key)
}

// New creates a new SecretsUsedInArgOrEnv rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
