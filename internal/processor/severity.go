package processor

import (
	"github.com/tinovyatkin/tally/internal/rules"
)

// SeverityOverride applies severity overrides from configuration.
// Allows users to downgrade warnings to info, upgrade info to errors, etc.
type SeverityOverride struct{}

// NewSeverityOverride creates a new severity override processor.
func NewSeverityOverride() *SeverityOverride {
	return &SeverityOverride{}
}

// Name returns the processor's identifier.
func (p *SeverityOverride) Name() string {
	return "severity-override"
}

// Process applies severity overrides from config.
func (p *SeverityOverride) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
	return transformViolations(violations, func(v rules.Violation) rules.Violation {
		// Get config for the violation's file
		cfg := ctx.ConfigForFile(v.Location.File)

		override := cfg.Rules.GetSeverity(v.RuleCode)
		if override == "" {
			return v
		}

		sev, err := rules.ParseSeverity(override)
		if err != nil {
			// Invalid severity in config - keep original
			return v
		}

		v.Severity = sev
		return v
	})
}
