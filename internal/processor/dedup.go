package processor

import (
	"fmt"
	"path/filepath"

	"github.com/tinovyatkin/tally/internal/rules"
)

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
	seen := make(map[string]bool)
	return filterViolations(violations, func(v rules.Violation) bool {
		// Key: file:line:rule (normalize path for cross-platform deduplication)
		key := fmt.Sprintf("%s:%d:%s", filepath.ToSlash(v.Location.File), v.Location.Start.Line, v.RuleCode)
		if seen[key] {
			return false
		}
		seen[key] = true
		return true
	})
}
