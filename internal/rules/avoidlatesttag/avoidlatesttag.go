// Package avoidlatesttag implements hadolint DL3007.
// This rule warns when a base image uses the :latest tag,
// which can lead to unpredictable and non-reproducible builds.
package avoidlatesttag

import (
	"fmt"
	"strings"

	"github.com/distribution/reference"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// Rule implements the DL3007 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3007",
		Name:            "Avoid using :latest tag",
		Description:     "Using :latest is prone to errors if the image will ever update. Pin the version explicitly to a release tag.",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3007",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "reproducibility",
		IsExperimental:  false,
	}
}

// Check runs the DL3007 rule.
// It warns when a FROM instruction uses an image with the :latest tag.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		return nil
	}

	violations := make([]rules.Violation, 0, 4)
	for info := range sem.ExternalImageStages() {
		if !usesLatestTag(info.Stage.BaseName) {
			continue
		}
		loc := rules.NewLocationFromRanges(input.File, info.Stage.Location)
		imageName := getImageName(info.Stage.BaseName)
		violations = append(violations, rules.NewViolation(
			loc,
			r.Metadata().Code,
			fmt.Sprintf(
				"using :latest tag for image %q is prone to errors; pin a specific version instead (e.g., %s:22.04)",
				info.Stage.BaseName,
				imageName,
			),
			r.Metadata().DefaultSeverity,
		).WithDocURL(r.Metadata().DocURL).WithDetail(
			"The :latest tag can change at any time, potentially breaking builds "+
				"or introducing unexpected behavior. Use a specific version tag for reproducibility.",
		))
	}

	return violations
}

// usesLatestTag checks if an image reference uses the :latest tag.
func usesLatestTag(image string) bool {
	// Try to parse as a normalized named reference
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		// Can't parse - check for simple :latest suffix
		return strings.HasSuffix(image, ":latest")
	}

	// Check if it has a tag
	if tagged, ok := named.(reference.NamedTagged); ok {
		return tagged.Tag() == "latest"
	}
	return false
}

// getImageName extracts just the image name without the tag.
func getImageName(image string) string {
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		// Fallback: strip the tag manually
		if idx := strings.LastIndex(image, ":"); idx != -1 {
			return image[:idx]
		}
		return image
	}
	return reference.FamiliarName(named)
}

// New creates a new DL3007 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
