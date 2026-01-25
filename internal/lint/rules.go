package lint

import (
	"fmt"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/dockerfile"
)

// Issue represents a linting issue found in a Dockerfile
type Issue struct {
	// Rule is the rule identifier (e.g., "max-lines")
	Rule string `json:"rule"`
	// Line is the line number where the issue was found (0 for file-level issues)
	Line int `json:"line"`
	// Message is the human-readable description of the issue
	Message string `json:"message"`
	// Severity is the issue severity (error, warning, info)
	Severity string `json:"severity"`
}

// FileResult contains the linting results for a single file
type FileResult struct {
	// File is the path to the Dockerfile
	File string `json:"file"`
	// Lines is the total number of lines in the file
	Lines int `json:"lines"`
	// Issues is the list of linting issues found
	Issues []Issue `json:"issues"`
}

// CheckMaxLines checks if the Dockerfile exceeds the maximum line count.
//
// The rule supports the following options:
//   - Max: maximum number of lines allowed (0 = disabled)
//   - SkipBlankLines: exclude blank lines from the count
//   - SkipComments: exclude comment lines from the count
//
// Returns nil if the rule is disabled or the file passes the check.
func CheckMaxLines(result *dockerfile.ParseResult, rule config.MaxLinesRule) *Issue {
	if !rule.Enabled() {
		return nil
	}

	// Calculate effective line count based on skip options
	effectiveLines := result.TotalLines
	skipped := 0

	if rule.SkipBlankLines {
		effectiveLines -= result.BlankLines
		skipped += result.BlankLines
	}

	if rule.SkipComments {
		effectiveLines -= result.CommentLines
		skipped += result.CommentLines
	}

	if effectiveLines > rule.Max {
		msg := fmt.Sprintf("file has %d lines", effectiveLines)
		if skipped > 0 {
			msg += fmt.Sprintf(" (excluding %d skipped)", skipped)
		}
		msg += fmt.Sprintf(", maximum allowed is %d", rule.Max)

		return &Issue{
			Rule:     "max-lines",
			Line:     0, // File-level issue
			Message:  msg,
			Severity: "error",
		}
	}
	return nil
}
