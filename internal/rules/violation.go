package rules

import "github.com/moby/buildkit/frontend/dockerfile/parser"

// FixSafety categorizes how reliable a fix is.
type FixSafety int

const (
	// FixSafe means the fix is always correct and won't change behavior.
	// These fixes can be applied automatically without review.
	FixSafe FixSafety = iota

	// FixSuggestion means the fix is likely correct but may need review.
	// Examples: apt search → apt-cache search (different output format).
	FixSuggestion

	// FixUnsafe means the fix might change behavior significantly.
	// These require explicit --fix-unsafe flag to apply.
	FixUnsafe
)

// String returns the string representation of FixSafety.
func (s FixSafety) String() string {
	switch s {
	case FixSafe:
		return "safe"
	case FixSuggestion:
		return "suggestion"
	case FixUnsafe:
		return "unsafe"
	default:
		return "unknown"
	}
}

// SuggestedFix represents a structured edit hint for auto-fix suggestions.
// It describes what text to replace and what to replace it with.
//
// Fixes can be synchronous (Edits populated immediately) or asynchronous
// (NeedsResolve=true, edits computed later by a FixResolver).
type SuggestedFix struct {
	// Description explains what this fix does.
	Description string `json:"description"`

	// Edits contains the actual text replacements to apply.
	// May be empty if NeedsResolve is true (populated by resolver).
	Edits []TextEdit `json:"edits,omitempty"`

	// Safety indicates how reliable this fix is.
	// Default (zero value) is FixSafe.
	Safety FixSafety `json:"safety,omitzero"`

	// IsPreferred marks this as the recommended fix when alternatives exist.
	IsPreferred bool `json:"isPreferred,omitzero"`

	// NeedsResolve indicates this fix requires async resolution.
	// When true, Edits is empty and ResolverID specifies which resolver to use.
	// Examples: fetching image digests, computing file checksums.
	NeedsResolve bool `json:"needsResolve,omitzero"`

	// ResolverID identifies which FixResolver should compute the edits.
	// Only used when NeedsResolve is true.
	ResolverID string `json:"resolverId,omitempty"`

	// ResolverData contains opaque data for the resolver.
	// Not serialized to JSON; used internally during fix application.
	ResolverData any `json:"-"`

	// Priority determines application order when multiple fixes exist.
	// Copied from rule's FixPriority. Lower = applied first.
	// Content fixes (priority 0) run before structural transforms (priority 100+).
	Priority int `json:"priority,omitzero"`
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

	// StageIndex tracks which Dockerfile stage this violation belongs to.
	// Used internally for merging async results; not serialized.
	StageIndex int `json:"-"`
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
