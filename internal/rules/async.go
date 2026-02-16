package rules

import "github.com/wharflab/tally/internal/async"

// AsyncRule is an optional interface for rules that require slow I/O (registry,
// network, filesystem). Rules implementing this interface participate in the
// async checks pipeline.
//
// PlanAsync is called during the planning phase (no I/O). It returns check
// requests that the async runtime will execute under budget control.
//
// The handler's OnSuccess returns []any where each element is a rules.Violation.
// This avoids an import cycle between the async and rules packages.
type AsyncRule interface {
	Rule
	PlanAsync(input LintInput) []async.CheckRequest
}
