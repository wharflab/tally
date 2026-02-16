package buildkit

import (
	"strings"

	"github.com/wharflab/tally/internal/rules"
)

// RedundantTargetPlatformRule implements the RedundantTargetPlatform linting rule.
// It detects FROM instructions where --platform explicitly specifies
// $TARGETPLATFORM, which is redundant as it's the default.
type RedundantTargetPlatformRule struct{}

// NewRedundantTargetPlatformRule creates a new RedundantTargetPlatform rule instance.
func NewRedundantTargetPlatformRule() *RedundantTargetPlatformRule {
	return &RedundantTargetPlatformRule{}
}

// Metadata returns the rule metadata.
func (r *RedundantTargetPlatformRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.BuildKitRulePrefix + "RedundantTargetPlatform",
		Name:            "Redundant TARGETPLATFORM",
		Description:     "Setting platform to $TARGETPLATFORM is redundant as this is the default behavior",
		DocURL:          "https://docs.docker.com/go/dockerfile/rule/redundant-target-platform/",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practices",
		IsExperimental:  false,
	}
}

// Check runs the RedundantTargetPlatform rule.
// It examines FROM instructions for --platform=$TARGETPLATFORM which is redundant.
func (r *RedundantTargetPlatformRule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation

	for _, stage := range input.Stages {
		// Check if the FROM has a --platform that just resolves to $TARGETPLATFORM
		platform := stage.Platform
		if platform == "" {
			continue
		}

		if isRedundantPlatform(platform) {
			loc := rules.NewLocationFromRanges(input.File, stage.Location)
			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				"Setting platform to $TARGETPLATFORM in FROM is redundant as this is the default behavior",
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL).WithDetail(
				"Remove --platform=$TARGETPLATFORM from the FROM instruction. "+
					"The build automatically targets the same platform as the build machine."))
		}
	}

	return violations
}

// isRedundantPlatform checks if a platform value is redundant.
// A platform is redundant if it resolves to just $TARGETPLATFORM without any
// other modifications or variables.
func isRedundantPlatform(platform string) bool {
	// Trim whitespace
	platform = strings.TrimSpace(platform)

	// Check for direct $TARGETPLATFORM or ${TARGETPLATFORM}
	if platform == "$TARGETPLATFORM" || platform == "${TARGETPLATFORM}" {
		return true
	}

	// Also handle variations with surrounding braces/quotes that still resolve
	// to just TARGETPLATFORM without any modification
	// e.g., "${TARGETPLATFORM:-}" would NOT be redundant (has default)
	// e.g., "${TARGETPLATFORM}" IS redundant
	return false
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewRedundantTargetPlatformRule())
}
