package hadolint

import (
	"fmt"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// DL3007Rule implements the DL3007 linting rule.
type DL3007Rule struct{}

// NewDL3007Rule creates a new DL3007 rule instance.
func NewDL3007Rule() *DL3007Rule {
	return &DL3007Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3007Rule) Metadata() rules.RuleMetadata {
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
func (r *DL3007Rule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		return nil
	}

	violations := make([]rules.Violation, 0, 4)
	for info := range sem.ExternalImageStages() {
		ref := parseImageRef(info.Stage.BaseName)
		// Can't parse or doesn't use :latest - skip
		// If the image has a digest, it's pinned regardless of tag
		if ref == nil || !ref.IsLatestTag() || ref.HasDigest() {
			continue
		}

		loc := rules.NewLocationFromRanges(input.File, info.Stage.Location)
		violations = append(violations, rules.NewViolation(
			loc,
			r.Metadata().Code,
			fmt.Sprintf(
				"using :latest tag for image %q is prone to errors; pin a specific version instead (e.g., %s:22.04)",
				info.Stage.BaseName,
				ref.FamiliarName(),
			),
			r.Metadata().DefaultSeverity,
		).WithDocURL(r.Metadata().DocURL).WithDetail(
			"The :latest tag can change at any time, potentially breaking builds "+
				"or introducing unexpected behavior. Use a specific version tag for reproducibility.",
		))
	}

	return violations
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3007Rule())
}
