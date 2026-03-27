// Package hadolint implements hadolint-compatible linting rules for Dockerfiles.
package hadolint

import (
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
)

// DL3002Rule implements the DL3002 linting rule.
type DL3002Rule struct{}

// NewDL3002Rule creates a new DL3002 rule instance.
func NewDL3002Rule() *DL3002Rule {
	return &DL3002Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3002Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3002",
		Name:            "Last USER should not be root",
		Description:     "Last USER should not be root to follow security best practices",
		DocURL:          rules.HadolintDocURL("DL3002"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
		IsExperimental:  false,
	}
}

// Check runs the DL3002 rule.
// It warns when the last USER instruction in a stage is root.
// Only checks the final stage (the one that will actually be run).
func (r *DL3002Rule) Check(input rules.LintInput) []rules.Violation {
	if len(input.Stages) == 0 {
		return nil
	}

	finalIdx := len(input.Stages) - 1

	fileFacts, _ := input.Facts.(*facts.FileFacts) //nolint:errcheck // nil-safe assertion
	if fileFacts == nil {
		return nil
	}

	sf := fileFacts.Stage(finalIdx)
	if sf == nil {
		return nil
	}

	// DL3002 specifically checks if the last USER is explicitly root.
	// If there's no USER instruction at all, hadolint does not warn.
	if sf.EffectiveUser == "" || !facts.IsRootUser(sf.EffectiveUser) {
		return nil
	}

	// Use the last UserCommand for location.
	lastUser := sf.UserCommands[len(sf.UserCommands)-1]
	loc := rules.NewLocationFromRanges(input.File, lastUser.Location())

	return []rules.Violation{
		rules.NewViolation(
			loc,
			r.Metadata().Code,
			"last USER should not be root; use a non-privileged user for better security",
			r.Metadata().DefaultSeverity,
		).WithDocURL(r.Metadata().DocURL).WithDetail(
			"Running containers as root increases the attack surface. " +
				"Create a non-privileged user and switch to it with USER instruction. " +
				"For example: RUN useradd -m appuser && USER appuser",
		),
	}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3002Rule())
}
