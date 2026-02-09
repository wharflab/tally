package semantic

import (
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Issue represents a semantic problem detected during model construction.
// This is similar to rules.Violation but without the dependency on the rules
// package to avoid import cycles. The check.go command converts these to
// rules.Violation before output.
type Issue struct {
	// Location is where the issue occurred (first range).
	Location parser.Range

	// File is the path to the Dockerfile.
	File string

	// Code is the rule code (e.g., "DL3024").
	Code string

	// Message is a human-readable description.
	Message string

	// DocURL links to documentation about this issue.
	DocURL string

	// Severity overrides the default severity for this issue.
	// Zero value (SeverityError) is the default for backward compatibility.
	Severity rules.Severity
}

// newIssue creates a new semantic issue.
func newIssue(file string, location parser.Range, code, message, docURL string) Issue {
	return Issue{
		File:     file,
		Location: location,
		Code:     code,
		Message:  message,
		DocURL:   docURL,
	}
}
