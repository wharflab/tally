package processor

import (
	"github.com/tinovyatkin/tally/internal/rules"
)

// EnableFilter removes violations for disabled rules.
// Uses config.Rules.IsEnabled() to check if a rule is enabled.
// If no configuration exists for a rule, it uses the rule's EnabledByDefault from metadata.
type EnableFilter struct {
	// registry is used to look up rule metadata for EnabledByDefault
	registry *rules.Registry
}

// NewEnableFilter creates a new enable filter processor.
func NewEnableFilter() *EnableFilter {
	return &EnableFilter{
		registry: rules.DefaultRegistry(),
	}
}

// Name returns the processor's identifier.
func (p *EnableFilter) Name() string {
	return "enable-filter"
}

// Process filters out violations for disabled rules.
func (p *EnableFilter) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
	return filterViolations(violations, func(v rules.Violation) bool {
		// Check config-based enable/disable
		enabled := ctx.Config.Rules.IsEnabled(v.RuleCode)

		if enabled != nil {
			// Config explicitly enables/disables this rule
			return *enabled
		}

		// No config - check rule's EnabledByDefault
		rule := p.registry.Get(v.RuleCode)
		if rule != nil {
			return rule.Metadata().EnabledByDefault
		}

		// Unknown rule - default to enabled (e.g., BuildKit warnings)
		return true
	})
}
