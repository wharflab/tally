// Package directive provides inline suppression directives for linting.
//
// This package implements comment-based suppression compatible with:
//   - tally:    # tally ignore=RULE1,RULE2 or # tally global ignore=...
//   - hadolint: # hadolint ignore=RULE1,RULE2 (migration compatibility)
//   - buildx:   # check=skip=RULE1,RULE2 (Docker buildx compatibility)
//
// Directives can be:
//   - Next-line: Affects the next non-comment line only
//   - Global: Affects the entire file
package directive

import (
	"math"
	"strings"
)

// DirectiveType indicates the scope of a directive.
type DirectiveType int

const (
	// TypeNextLine affects only the next non-comment line.
	TypeNextLine DirectiveType = iota
	// TypeGlobal affects the entire file.
	TypeGlobal
)

// String returns a human-readable name for the directive type.
func (t DirectiveType) String() string {
	switch t {
	case TypeNextLine:
		return "next-line"
	case TypeGlobal:
		return "global"
	default:
		return "unknown"
	}
}

// LineRange represents a range of lines affected by a directive.
// Line numbers are 0-based to match SourceMap conventions.
type LineRange struct {
	// Start is the 0-based line number (inclusive).
	Start int
	// End is the 0-based line number (inclusive).
	// For global directives, this is math.MaxInt.
	End int
}

// Contains returns true if the given 0-based line is within the range.
func (r LineRange) Contains(line int) bool {
	return line >= r.Start && line <= r.End
}

// GlobalRange returns a LineRange that covers the entire file.
func GlobalRange() LineRange {
	return LineRange{Start: 0, End: math.MaxInt}
}

// Directive represents a parsed inline suppression directive.
type Directive struct {
	// Type indicates whether this is a next-line or global directive.
	Type DirectiveType

	// Rules contains the rule codes to suppress.
	// A single-element slice containing "all" means suppress all rules.
	Rules []string

	// Line is the 0-based line number where the directive appears.
	Line int

	// AppliesTo is the range of lines affected by this directive.
	AppliesTo LineRange

	// Used is set to true when this directive suppresses at least one violation.
	// Used for unused directive detection.
	Used bool

	// RawText is the original comment text (for error messages).
	RawText string

	// Source indicates which format the directive used.
	Source DirectiveSource

	// Reason is an optional explanation for why the rule is being suppressed.
	// Extracted from `reason=...` in the directive comment.
	Reason string
}

// DirectiveSource identifies which syntax format was used.
type DirectiveSource string

const (
	// SourceTally indicates # tally ignore=... syntax.
	SourceTally DirectiveSource = "tally"
	// SourceHadolint indicates # hadolint ignore=... syntax.
	SourceHadolint DirectiveSource = "hadolint"
	// SourceBuildx indicates # check=skip=... syntax.
	SourceBuildx DirectiveSource = "buildx"
)

// SuppressesRule returns true if this directive suppresses the given rule code.
// Supports both namespaced (tally/max-lines) and non-namespaced (max-lines) forms.
func (d *Directive) SuppressesRule(ruleCode string) bool {
	for _, r := range d.Rules {
		if r == "all" || matchesRule(r, ruleCode) {
			return true
		}
	}
	return false
}

// matchesRule checks if a directive rule pattern matches a rule code.
// Supports:
//   - Exact match: "tally/max-lines" matches "tally/max-lines"
//   - Suffix match: "max-lines" matches "tally/max-lines"
//   - Prefix match: "tally/max-lines" matches "max-lines" (directive is more specific)
func matchesRule(pattern, ruleCode string) bool {
	if pattern == ruleCode {
		return true
	}

	// Check if pattern matches the suffix (rule name without namespace)
	// e.g., pattern "max-lines" should match ruleCode "tally/max-lines"
	if idx := strings.LastIndexByte(ruleCode, '/'); idx != -1 {
		if pattern == ruleCode[idx+1:] {
			return true
		}
	}

	// Check if ruleCode matches the suffix of pattern
	// e.g., pattern "tally/max-lines" should match ruleCode "max-lines"
	if idx := strings.LastIndexByte(pattern, '/'); idx != -1 {
		if ruleCode == pattern[idx+1:] {
			return true
		}
	}

	return false
}

// SuppressesLine returns true if this directive suppresses violations on the given line.
// Line is 0-based.
func (d *Directive) SuppressesLine(line int) bool {
	return d.AppliesTo.Contains(line)
}

// ParseResult contains all directives parsed from a file plus any errors.
type ParseResult struct {
	// Directives contains successfully parsed directives.
	Directives []Directive

	// Errors contains parse errors for malformed directives.
	Errors []ParseError
}

// ParseError represents an error parsing a directive.
type ParseError struct {
	// Line is the 0-based line number where the error occurred.
	Line int

	// Message describes what went wrong.
	Message string

	// RawText is the original comment text.
	RawText string
}
