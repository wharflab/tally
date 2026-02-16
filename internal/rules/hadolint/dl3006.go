package hadolint

import (
	"fmt"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// DL3006Rule implements the DL3006 linting rule.
type DL3006Rule struct{}

// NewDL3006Rule creates a new DL3006 rule instance.
func NewDL3006Rule() *DL3006Rule {
	return &DL3006Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3006Rule) Metadata() rules.RuleMetadata {
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
func (r *DL3006Rule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		return nil
	}

	violations := make([]rules.Violation, 0, 4)
	for info := range sem.ExternalImageStages() {
		ref := parseImageRef(info.Stage.BaseName)
		// Can't parse or has explicit version - skip
		if ref == nil || ref.HasExplicitVersion() {
			continue
		}

		loc := rules.NewLocationFromRanges(input.File, info.Stage.Location)
		violations = append(violations, rules.NewViolation(
			loc,
			r.Metadata().Code,
			fmt.Sprintf(
				"image %q does not have an explicit tag; pin a specific version (e.g., %s:22.04)",
				info.Stage.BaseName,
				ref.FamiliarName(),
			),
			r.Metadata().DefaultSeverity,
		).WithDocURL(r.Metadata().DocURL).WithDetail(
			"Untagged images default to :latest, which can change unexpectedly and break builds. "+
				"Always specify an explicit tag for reproducibility.",
		))
	}

	return violations
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3006Rule())
}
