package buildkit

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/tinovyatkin/tally/internal/rules"
)

// ExposeProtoCasingRule implements the ExposeProtoCasing linting rule.
// It checks that protocol names in EXPOSE instructions are lowercase
// (e.g., "tcp" instead of "TCP").
//
// This mirrors BuildKit's dispatchExpose logic from dockerfile2llb/convert.go
// but runs during the linting phase rather than during LLB conversion.
type ExposeProtoCasingRule struct{}

// NewExposeProtoCasingRule creates a new ExposeProtoCasing rule instance.
func NewExposeProtoCasingRule() *ExposeProtoCasingRule {
	return &ExposeProtoCasingRule{}
}

// Metadata returns the rule metadata.
func (r *ExposeProtoCasingRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.BuildKitRulePrefix + "ExposeProtoCasing",
		Name:            "Expose Proto Casing",
		Description:     linter.RuleExposeProtoCasing.Description,
		DocURL:          linter.RuleExposeProtoCasing.URL,
		DefaultSeverity: rules.SeverityWarning,
		Category:        "style",
		IsExperimental:  false,
	}
}

// Check runs the ExposeProtoCasing rule.
// It scans EXPOSE instructions for port specifications with non-lowercase protocols.
// One violation is reported per non-lowercase port, matching BuildKit's behavior.
//
// Original source:
// https://github.com/moby/buildkit/blob/master/frontend/dockerfile/dockerfile2llb/convert_expose.go
func (r *ExposeProtoCasingRule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			expose, ok := cmd.(*instructions.ExposeCommand)
			if !ok {
				continue
			}

			for _, port := range expose.Ports {
				_, proto, hasProto := strings.Cut(port, "/")
				if !hasProto {
					continue
				}
				if proto == strings.ToLower(proto) {
					continue
				}

				loc := rules.NewLocationFromRanges(input.File, expose.Location())
				msg := linter.RuleExposeProtoCasing.Format(port)
				violations = append(violations, rules.NewViolation(
					loc, r.Metadata().Code, msg, r.Metadata().DefaultSeverity,
				).WithDocURL(r.Metadata().DocURL))
			}
		}
	}

	return violations
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewExposeProtoCasingRule())
}
