package processor

import (
	"github.com/tinovyatkin/tally/internal/rules"
)

// EnableFilter removes violations for disabled rules.
// Filters out violations with severity="off".
// Also respects Include/Exclude patterns from config.
type EnableFilter struct {
	// registry is used to look up rule metadata
	registry *rules.Registry
}

// NewEnableFilter creates a new enable filter processor.
// Uses the default registry. For testing, use NewEnableFilterWithRegistry.
func NewEnableFilter() *EnableFilter {
	return NewEnableFilterWithRegistry(rules.DefaultRegistry())
}

// NewEnableFilterWithRegistry creates an enable filter with a custom registry.
func NewEnableFilterWithRegistry(registry *rules.Registry) *EnableFilter {
	if registry == nil {
		registry = rules.DefaultRegistry()
	}
	return &EnableFilter{
		registry: registry,
	}
}

// Name returns the processor's identifier.
func (p *EnableFilter) Name() string {
	return "enable-filter"
}

// Process filters out violations for disabled rules.
// Rules are disabled if:
//  1. Severity is "off" (after SeverityOverride has run)
//  2. Excluded by Include/Exclude patterns
func (p *EnableFilter) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
	return filterViolations(violations, func(v rules.Violation) bool {
		// Filter out violations with severity="off"
		// (SeverityOverride runs before this processor)
		if v.Severity == rules.SeverityOff {
			return false
		}

		// Check Include/Exclude patterns from config
		cfg := ctx.ConfigForFile(v.Location.File)
		if cfg != nil {
			enabled := cfg.Rules.IsEnabled(v.RuleCode)
			if enabled != nil {
				return *enabled
			}
		}

		// No explicit Include/Exclude - rule is enabled
		return true
	})
}
