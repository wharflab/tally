package processor

import (
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
)

// PathNormalization converts file paths to forward slashes for cross-platform consistency.
// This ensures output is identical regardless of OS (Windows vs Unix).
type PathNormalization struct{}

// NewPathNormalization creates a new path normalization processor.
func NewPathNormalization() *PathNormalization {
	return &PathNormalization{}
}

// Name returns the processor's identifier.
func (p *PathNormalization) Name() string {
	return "path-normalization"
}

// Process normalizes all file paths to use forward slashes.
func (p *PathNormalization) Process(violations []rules.Violation, _ *Context) []rules.Violation {
	return transformViolations(violations, func(v rules.Violation) rules.Violation {
		// Replace backslashes with forward slashes for cross-platform consistency
		v.Location.File = strings.ReplaceAll(v.Location.File, "\\", "/")
		return v
	})
}
