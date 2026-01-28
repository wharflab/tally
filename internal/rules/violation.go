package rules

import "github.com/moby/buildkit/frontend/dockerfile/parser"

// SuggestedFix represents a structured edit hint for auto-fix suggestions.
// It describes what text to replace and what to replace it with.
type SuggestedFix struct {
	// Description explains what this fix does.
	Description string `json:"description"`
	// Edits contains the actual text replacements to apply.
	Edits []TextEdit `json:"edits"`
}

// TextEdit represents a single text replacement in a file.
type TextEdit struct {
	// Location specifies where to apply the edit.
	Location Location `json:"location"`
	// NewText is the text to insert/replace with. Empty string means delete.
	NewText string `json:"newText"`
}

// Violation represents a single linting violation.
// This extends BuildKit's subrequests/lint.Warning with:
//   - Severity levels (BuildKit treats all as warnings)
//   - Inline file path (BuildKit uses SourceIndex into separate Sources array)
//   - SuggestedFix for auto-fix hints
//   - SourceCode snippet
//
// See: github.com/moby/buildkit/frontend/subrequests/lint.Warning
type Violation struct {
	// Location specifies where the violation occurred.
	Location Location `json:"location"`

	// RuleCode is the unique identifier for the rule (e.g., "DL3006", "max-lines").
	RuleCode string `json:"rule"`

	// Message is a human-readable description of the issue.
	Message string `json:"message"`

	// Detail provides additional context (optional).
	Detail string `json:"detail,omitempty"`

	// Severity indicates how critical this violation is.
	Severity Severity `json:"severity"`

	// DocURL links to documentation about this rule (optional).
	DocURL string `json:"docUrl,omitempty"`

	// SourceCode is the source snippet where the violation occurred (optional).
	// Populated by post-processing; rules don't need to set this.
	SourceCode string `json:"sourceCode,omitempty"`

	// SuggestedFix provides a structured fix hint (optional).
	// Supports "auto-fix suggestion" without auto-applying.
	SuggestedFix *SuggestedFix `json:"suggestedFix,omitempty"`
}

// NewViolation creates a new violation with the minimum required fields.
func NewViolation(loc Location, ruleCode, message string, severity Severity) Violation {
	return Violation{
		Location: loc,
		RuleCode: ruleCode,
		Message:  message,
		Severity: severity,
	}
}

// BuildKitRulePrefix is the namespace prefix for rules from BuildKit's linter.
const BuildKitRulePrefix = "buildkit/"

// TallyRulePrefix is the namespace prefix for tally's own rules.
const TallyRulePrefix = "tally/"

// HadolintRulePrefix is the namespace prefix for Hadolint-compatible rules.
const HadolintRulePrefix = "hadolint/"

// NewViolationFromBuildKitWarning converts BuildKit linter callback parameters
// to our Violation type. This bridges BuildKit's linter.LintWarnFunc with our
// output schema.
//
// Parameters match linter.LintWarnFunc: (rulename, description, url, fmtmsg, location)
// The rule code is automatically namespaced with "buildkit/" prefix.
func NewViolationFromBuildKitWarning(
	file string,
	ruleName string,
	description string,
	url string,
	message string,
	location []parser.Range,
) Violation {
	// Use first range for location, or file-level if none
	var loc Location
	if len(location) > 0 {
		loc = NewLocationFromRange(file, location[0])
	} else {
		loc = NewFileLocation(file)
	}

	return Violation{
		Location: loc,
		RuleCode: BuildKitRulePrefix + ruleName,
		Message:  message,
		Detail:   description,
		Severity: SeverityWarning, // BuildKit warnings map to our warning severity
		DocURL:   url,
	}
}

// WithDetail adds a detail message to the violation.
func (v Violation) WithDetail(detail string) Violation {
	v.Detail = detail
	return v
}

// WithDocURL adds a documentation URL to the violation.
func (v Violation) WithDocURL(url string) Violation {
	v.DocURL = url
	return v
}

// WithSourceCode adds source code snippet to the violation.
func (v Violation) WithSourceCode(code string) Violation {
	v.SourceCode = code
	return v
}

// WithSuggestedFix adds a fix suggestion to the violation.
func (v Violation) WithSuggestedFix(fix *SuggestedFix) Violation {
	v.SuggestedFix = fix
	return v
}

// File returns the file path from the location.
func (v Violation) File() string {
	return v.Location.File
}

// Line returns the starting line number (for backward compatibility).
func (v Violation) Line() int {
	return v.Location.Start.Line
}
