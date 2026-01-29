package processor

import (
	"github.com/tinovyatkin/tally/internal/rules"
)

// SeverityOverride applies severity overrides from configuration.
// Allows users to downgrade warnings to info, upgrade info to errors, etc.
// Also auto-enables rules with DefaultSeverity="off" when config is provided.
type SeverityOverride struct {
	registry *rules.Registry
}

// NewSeverityOverride creates a new severity override processor.
func NewSeverityOverride() *SeverityOverride {
	return NewSeverityOverrideWithRegistry(rules.DefaultRegistry())
}

// NewSeverityOverrideWithRegistry creates a severity override processor with a custom registry.
func NewSeverityOverrideWithRegistry(registry *rules.Registry) *SeverityOverride {
	if registry == nil {
		registry = rules.DefaultRegistry()
	}
	return &SeverityOverride{
		registry: registry,
	}
}

// Name returns the processor's identifier.
func (p *SeverityOverride) Name() string {
	return "severity-override"
}

// Process applies severity overrides from config.
// Also auto-enables rules with DefaultSeverity="off" when config is provided.
func (p *SeverityOverride) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
	return transformViolations(violations, func(v rules.Violation) rules.Violation {
		// Get config for the violation's file
		cfg := ctx.ConfigForFile(v.Location.File)
		if cfg == nil {
			return v
		}

		override := cfg.Rules.GetSeverity(v.RuleCode)
		if override != "" {
			// Explicit severity override
			sev, err := rules.ParseSeverity(override)
			if err != nil {
				// Invalid severity in config - keep original
				return v
			}
			v.Severity = sev
			return v
		}

		// Auto-enable: If rule has DefaultSeverity="off" but config is provided (options),
		// implicitly enable with "warning" severity
		ruleConfig := cfg.Rules.Get(v.RuleCode)
		if ruleConfig != nil && len(ruleConfig.Options) > 0 {
			// Config options provided - check if rule is "off" by default
			rule := p.registry.Get(v.RuleCode)
			if rule != nil && rule.Metadata().DefaultSeverity == rules.SeverityOff {
				// Auto-enable with warning severity
				v.Severity = rules.SeverityWarning
			}
		}

		return v
	})
}
