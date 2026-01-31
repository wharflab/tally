// Package testutil provides test helpers for the Dockerfile linter.
package testutil

import (
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// ParseDockerfile parses a Dockerfile from a string using the full parsing pipeline.
// Returns the complete ParseResult including AST, Stages, MetaArgs, and Warnings.
//
// Use this when you need the full parsed result (e.g., testing parser features).
// For rule testing, prefer MakeLintInput which creates a ready-to-use LintInput.
func ParseDockerfile(tb testing.TB, content string) *dockerfile.ParseResult {
	tb.Helper()

	result, err := dockerfile.Parse(strings.NewReader(content), nil)
	if err != nil {
		tb.Fatalf("failed to parse Dockerfile: %v", err)
	}
	return result
}

// MakeLintInput creates a LintInput for testing a rule.
// Parses the Dockerfile content and constructs the input struct with full
// BuildKit instruction parsing including Stages and MetaArgs.
func MakeLintInput(tb testing.TB, file, content string) rules.LintInput {
	tb.Helper()

	result, err := dockerfile.Parse(strings.NewReader(content), nil)
	if err != nil {
		tb.Fatalf("failed to parse Dockerfile: %v", err)
	}

	return rules.LintInput{
		File:     file,
		AST:      result.AST,
		Stages:   result.Stages,
		MetaArgs: result.MetaArgs,
		Source:   result.Source,
		Context:  nil, // v1.0 doesn't require context
		Config:   nil, // Set by individual tests if needed
	}
}

// MakeLintInputWithConfig creates a LintInput with rule configuration.
func MakeLintInputWithConfig(tb testing.TB, file, content string, config any) rules.LintInput {
	tb.Helper()

	input := MakeLintInput(tb, file, content)
	input.Config = config
	return input
}

// MakeLintInputWithSemantic creates a LintInput with the semantic model.
// This is needed for rules that use semantic analysis (package tracking, etc.).
func MakeLintInputWithSemantic(tb testing.TB, file, content string) rules.LintInput {
	tb.Helper()

	result, err := dockerfile.Parse(strings.NewReader(content), nil)
	if err != nil {
		tb.Fatalf("failed to parse Dockerfile: %v", err)
	}

	// Build semantic model
	sem := semantic.NewBuilder(result, nil, file).Build()

	return rules.LintInput{
		File:     file,
		AST:      result.AST,
		Stages:   result.Stages,
		MetaArgs: result.MetaArgs,
		Source:   result.Source,
		Semantic: sem,
		Context:  nil,
		Config:   nil,
	}
}

// RuleTestCase defines a test case for table-driven rule tests.
type RuleTestCase struct {
	// Name is the test case name.
	Name string

	// Content is the Dockerfile content to lint.
	Content string

	// Config is the optional rule configuration.
	Config any

	// WantViolations is the expected number of violations.
	// Use -1 to skip the count check.
	WantViolations int

	// WantCodes is the expected rule codes in violation order (for detailed checks).
	WantCodes []string

	// WantMessages are substrings expected in violation messages.
	WantMessages []string
}

// RunRuleTests runs a table of test cases against a rule.
func RunRuleTests(t *testing.T, rule rules.Rule, cases []RuleTestCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			input := MakeLintInputWithConfig(t, "Dockerfile", tc.Content, tc.Config)
			violations := rule.Check(input)

			// Check violation count
			if tc.WantViolations >= 0 && len(violations) != tc.WantViolations {
				t.Errorf("got %d violations, want %d", len(violations), tc.WantViolations)
				for i, v := range violations {
					t.Logf("  [%d] %s: %s", i, v.RuleCode, v.Message)
				}
			}

			// Check violation codes
			if len(tc.WantCodes) > 0 {
				if len(violations) != len(tc.WantCodes) {
					t.Errorf("got %d violations, want %d", len(violations), len(tc.WantCodes))
				} else {
					for i, code := range tc.WantCodes {
						if violations[i].RuleCode != code {
							t.Errorf("violation[%d].RuleCode = %q, want %q", i, violations[i].RuleCode, code)
						}
					}
				}
			}

			// Check message substrings
			if len(tc.WantMessages) > 0 {
				for i, msg := range tc.WantMessages {
					if i >= len(violations) {
						t.Errorf(
							"expected violation[%d] with message containing %q, but only got %d violations",
							i,
							msg,
							len(violations),
						)
						continue
					}
					if !strings.Contains(violations[i].Message, msg) {
						t.Errorf("violation[%d].Message = %q, want substring %q", i, violations[i].Message, msg)
					}
				}
			}
		})
	}
}

// AssertNoViolations fails the test if there are any violations.
func AssertNoViolations(tb testing.TB, violations []rules.Violation) {
	tb.Helper()
	if len(violations) > 0 {
		tb.Errorf("expected no violations, got %d:", len(violations))
		for _, v := range violations {
			tb.Logf("  - %s at line %d: %s", v.RuleCode, v.Line(), v.Message)
		}
	}
}

// AssertViolationCount fails if the violation count doesn't match.
func AssertViolationCount(tb testing.TB, violations []rules.Violation, want int) {
	tb.Helper()
	if len(violations) != want {
		tb.Errorf("got %d violations, want %d", len(violations), want)
		for _, v := range violations {
			tb.Logf("  - %s at line %d: %s", v.RuleCode, v.Line(), v.Message)
		}
	}
}
