package processor

import (
	"path/filepath"

	"github.com/tinovyatkin/tally/internal/rules"
)

// violationKey uniquely identifies a violation for deduplication.
type violationKey struct {
	file string
	line int
	rule string
}

// Deduplication removes duplicate violations.
// Two violations are considered duplicates if they have the same file, line, and rule code.
// This handles cases where multiple rules report the same issue or the same rule
// reports the same issue multiple times.
type Deduplication struct{}

// NewDeduplication creates a new deduplication processor.
func NewDeduplication() *Deduplication {
	return &Deduplication{}
}

// Name returns the processor's identifier.
func (p *Deduplication) Name() string {
	return "deduplication"
}

// Process removes duplicate violations.
// Keeps the first occurrence of each unique (file, line, rule) combination.
func (p *Deduplication) Process(violations []rules.Violation, _ *Context) []rules.Violation {
	seen := make(map[violationKey]struct{})
	return filterViolations(violations, func(v rules.Violation) bool {
		key := violationKey{
			file: filepath.ToSlash(v.Location.File),
			line: v.Location.Start.Line,
			rule: v.RuleCode,
		}
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
		return true
	})
}
