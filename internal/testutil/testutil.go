// Package testutil provides test helpers for the Dockerfile linter.
package testutil

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
)

// ParseDockerfile parses a Dockerfile from a string.
// Returns the AST result and source lines for use in tests.
func ParseDockerfile(tb testing.TB, content string) *parser.Result {
	tb.Helper()

	result, err := parser.Parse(strings.NewReader(content))
	if err != nil {
		tb.Fatalf("failed to parse Dockerfile: %v", err)
	}
	return result
}

// MakeLintInput creates a LintInput for testing a rule.
// Parses the Dockerfile content and constructs the input struct with full
// BuildKit instruction parsing including Stages, MetaArgs, and LineStats.
func MakeLintInput(tb testing.TB, file, content string) rules.LintInput {
	tb.Helper()

	result, err := dockerfile.Parse(strings.NewReader(content))
	if err != nil {
		tb.Fatalf("failed to parse Dockerfile: %v", err)
	}
	lines := strings.Split(content, "\n")

	return rules.LintInput{
		File:     file,
		AST:      result.AST,
		Stages:   result.Stages,
		MetaArgs: result.MetaArgs,
		Source:   result.Source,
		Lines:    lines,
		LineStats: rules.LineStats{
			Total:    result.TotalLines,
			Blank:    result.BlankLines,
			Comments: result.CommentLines,
		},
		Context: nil, // v1.0 doesn't require context
		Config:  nil, // Set by individual tests if needed
	}
}

// MakeLintInputWithConfig creates a LintInput with rule configuration.
func MakeLintInputWithConfig(tb testing.TB, file, content string, config any) rules.LintInput {
	tb.Helper()

	input := MakeLintInput(tb, file, content)
	input.Config = config
	return input
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

// AssertViolationAt fails if there's no violation at the specified line with the given code.
func AssertViolationAt(tb testing.TB, violations []rules.Violation, line int, code string) {
	tb.Helper()
	for _, v := range violations {
		if v.Line() == line && v.RuleCode == code {
			return // Found
		}
	}
	tb.Errorf("expected violation %q at line %d, not found", code, line)
	tb.Logf("violations:")
	for _, v := range violations {
		tb.Logf("  - %s at line %d: %s", v.RuleCode, v.Line(), v.Message)
	}
}

// CountLines counts total lines in the content.
func CountLines(content string) int {
	if content == "" {
		return 0
	}
	return len(strings.Split(content, "\n"))
}

// CountBlankLines counts blank/whitespace-only lines.
func CountBlankLines(content string) int {
	count := 0
	for line := range strings.SplitSeq(content, "\n") {
		if strings.TrimSpace(line) == "" {
			count++
		}
	}
	return count
}

// CountCommentLines counts lines starting with # (comments).
func CountCommentLines(content string) int {
	count := 0
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			count++
		}
	}
	return count
}
