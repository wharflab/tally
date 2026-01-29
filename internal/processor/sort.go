package processor

import (
	"github.com/tinovyatkin/tally/internal/reporter"
	"github.com/tinovyatkin/tally/internal/rules"
)

// Sorting ensures stable, deterministic output ordering.
// Order: file path, then line number, then column, then rule code.
// This ensures identical output across runs and platforms.
type Sorting struct{}

// NewSorting creates a new sorting processor.
func NewSorting() *Sorting {
	return &Sorting{}
}

// Name returns the processor's identifier.
func (p *Sorting) Name() string {
	return "sorting"
}

// Process sorts violations in a stable order.
// Uses the existing reporter.SortViolations implementation.
func (p *Sorting) Process(violations []rules.Violation, _ *Context) []rules.Violation {
	// reporter.SortViolations returns a new sorted slice
	return reporter.SortViolations(violations)
}
