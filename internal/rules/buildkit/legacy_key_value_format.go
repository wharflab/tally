package buildkit

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/wharflab/tally/internal/rules"
)

// LegacyKeyValueFormatRule implements the LegacyKeyValueFormat linting rule.
// It detects ENV and LABEL instructions that use the deprecated whitespace-separated
// format (e.g., "ENV key value") instead of the modern equals format (e.g., "ENV key=value").
//
// This mirrors BuildKit's dispatchEnv/dispatchLabel logic from dockerfile2llb/convert.go
// but runs during the linting phase rather than during LLB conversion.
type LegacyKeyValueFormatRule struct{}

// NewLegacyKeyValueFormatRule creates a new LegacyKeyValueFormat rule instance.
func NewLegacyKeyValueFormatRule() *LegacyKeyValueFormatRule {
	return &LegacyKeyValueFormatRule{}
}

// Metadata returns the rule metadata.
func (r *LegacyKeyValueFormatRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.BuildKitRulePrefix + "LegacyKeyValueFormat",
		Name:            "Legacy Key/Value Format",
		Description:     linter.RuleLegacyKeyValueFormat.Description,
		DocURL:          rules.BuildKitDocURL("LegacyKeyValueFormat"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "style",
		IsExperimental:  false,
		// FixPriority 91 ensures semantic rules like prefer-package-cache-mounts (priority 90)
		// can delete an ENV instruction before this rule tries to reformat it.
		FixPriority: 91,
	}
}

// Check runs the LegacyKeyValueFormat rule.
// It scans ENV and LABEL instructions for key-value pairs using the legacy whitespace format.
func (r *LegacyKeyValueFormatRule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.EnvCommand:
				for _, kv := range c.Env {
					if kv.NoDelim {
						loc := rules.NewLocationFromRanges(input.File, c.Location())
						msg := linter.RuleLegacyKeyValueFormat.Format(c.Name())
						violations = append(violations, rules.NewViolation(
							loc, r.Metadata().Code, msg, r.Metadata().DefaultSeverity,
						).WithDocURL(r.Metadata().DocURL))
						break // one violation per instruction, matching BuildKit behavior
					}
				}
			case *instructions.LabelCommand:
				for _, kv := range c.Labels {
					if kv.NoDelim {
						loc := rules.NewLocationFromRanges(input.File, c.Location())
						msg := linter.RuleLegacyKeyValueFormat.Format(c.Name())
						violations = append(violations, rules.NewViolation(
							loc, r.Metadata().Code, msg, r.Metadata().DefaultSeverity,
						).WithDocURL(r.Metadata().DocURL))
						break // one violation per instruction
					}
				}
			}
		}
	}

	return violations
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewLegacyKeyValueFormatRule())
}
