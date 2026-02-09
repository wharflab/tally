package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

// PreferVEXAttestationRule flags COPY instructions that embed OpenVEX documents
// (typically *.vex.json) into the image filesystem and recommends attaching VEX
// as an OCI attestation instead.
type PreferVEXAttestationRule struct{}

// NewPreferVEXAttestationRule creates a new prefer-vex-attestation rule instance.
func NewPreferVEXAttestationRule() *PreferVEXAttestationRule {
	return &PreferVEXAttestationRule{}
}

// Metadata returns the rule metadata.
func (r *PreferVEXAttestationRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.TallyRulePrefix + "prefer-vex-attestation",
		Name:            "Prefer VEX attestation",
		Description:     "Prefer attaching OpenVEX as an OCI attestation instead of copying VEX JSON into the image",
		DocURL:          "https://github.com/tinovyatkin/tally/blob/main/docs/rules/tally/prefer-vex-attestation.md",
		DefaultSeverity: rules.SeverityInfo,
		Category:        "security",
		IsExperimental:  false,
	}
}

// Check runs the prefer-vex-attestation rule.
func (r *PreferVEXAttestationRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	var violations []rules.Violation

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			c, ok := cmd.(*instructions.CopyCommand)
			if !ok {
				continue
			}

			for _, src := range c.SourcePaths {
				if !isVEXJSONCopySource(src) {
					continue
				}

				loc := rules.NewLocationFromRanges(input.File, c.Location())
				v := rules.NewViolation(
					loc,
					meta.Code,
					fmt.Sprintf("prefer attaching VEX as an OCI attestation instead of copying %q into the image", src),
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"VEX documents are supply-chain metadata. Embedding them in the runtime image requires rebuilding to update statements and makes " +
						"discovery less consistent. Attach OpenVEX as an OCI attestation (in-toto predicate) instead.",
				)
				violations = append(violations, v)
			}
		}
	}

	return violations
}

func isVEXJSONCopySource(src string) bool {
	s := strings.ToLower(strings.TrimSpace(src))
	return strings.HasSuffix(s, ".vex.json")
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferVEXAttestationRule())
}
