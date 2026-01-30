// Package pinimageversions implements hadolint DL3006.
// This rule warns when a base image does not have an explicit tag,
// which can lead to non-reproducible builds.
package pinimageversions

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/distribution/reference"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Rule implements the DL3006 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3006",
		Name:            "Pin base image versions",
		Description:     "Always tag the version of an image explicitly to ensure reproducible builds",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3006",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "reproducibility",
		IsExperimental:  false,
	}
}

// Check runs the DL3006 rule.
// It warns when a FROM instruction uses an image without an explicit tag.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	// Build a set of stage names for quick lookup
	stageNames := make(map[string]bool)
	for i, stage := range input.Stages {
		if stage.Name != "" {
			stageNames[strings.ToLower(stage.Name)] = true
		}
		// Numeric index is also valid
		stageNames[strconv.Itoa(i)] = true
	}

	var violations []rules.Violation

	for _, stage := range input.Stages {
		// Skip scratch - it's a special "no base" image
		if stage.BaseName == "scratch" {
			continue
		}

		// Skip stage references (FROM stagename)
		if stageNames[strings.ToLower(stage.BaseName)] {
			continue
		}

		// Check if image has an explicit tag
		if !hasExplicitTag(stage.BaseName) {
			loc := rules.NewLocationFromRanges(input.File, stage.Location)
			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				fmt.Sprintf(
					"image %q does not have an explicit tag; pin a specific version (e.g., %s:22.04)",
					stage.BaseName,
					stage.BaseName,
				),
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL).WithDetail(
				"Untagged images default to :latest, which can change unexpectedly and break builds. "+
					"Always specify an explicit tag for reproducibility.",
			))
		}
	}

	return violations
}

// hasExplicitTag checks if an image reference has an explicit tag or digest.
// Returns true if the image has a tag (ubuntu:22.04) or digest (ubuntu@sha256:...).
// Returns false if the image has no tag (ubuntu), which defaults to :latest.
func hasExplicitTag(image string) bool {
	// Try to parse as a normalized named reference
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		// Can't parse - conservatively assume it has a tag
		return true
	}

	// Check if it has a tag or digest using interface assertions
	// NamedTagged is more specific than Tagged, so check order matters
	if _, ok := named.(reference.NamedTagged); ok {
		return true
	}
	if _, ok := named.(reference.Tagged); ok {
		return true
	}
	if _, ok := named.(reference.Digested); ok {
		return true
	}
	return false
}

// New creates a new DL3006 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
