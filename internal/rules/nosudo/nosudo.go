// Package nosudo implements hadolint DL3004.
// This rule warns when sudo is used in RUN instructions,
// as it has unpredictable behavior in containers.
package nosudo

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/shell"
)

// Rule implements the DL3004 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3004",
		Name:            "Do not use sudo",
		Description:     "Do not use sudo as it has unpredictable behavior in containers",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3004",
		DefaultSeverity: rules.SeverityError,
		Category:        "security",
		IsExperimental:  false,
	}
}

// Check runs the DL3004 rule.
// It warns when any RUN instruction contains a sudo command.
// Skips analysis for stages using non-POSIX shells (e.g., PowerShell).
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation
	meta := r.Metadata()

	// Get semantic model for shell variant info
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}

	for stageIdx, stage := range input.Stages {
		// Get shell variant for this stage
		shellVariant := shell.VariantBash
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				// Skip shell analysis for non-POSIX shells
				if shellVariant.IsNonPOSIX() {
					continue
				}
			}
		}

		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}

			// Check if the command contains sudo using the shell package
			cmdStr := getRunCommandString(run)
			if shell.ContainsCommandWithVariant(cmdStr, "sudo", shellVariant) {
				loc := rules.NewLocationFromRanges(input.File, run.Location())
				violations = append(violations, rules.NewViolation(
					loc,
					meta.Code,
					"do not use sudo in RUN commands; it has unpredictable TTY and signal handling",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"sudo is designed for interactive use and doesn't work reliably in containers. "+
						"Instead, use the USER instruction to switch users, or run specific commands "+
						"as a different user with 'su -c' if necessary.",
				))
			}
		}
	}

	return violations
}

// getRunCommandString extracts the command string from a RUN instruction.
// Handles both shell form (RUN cmd) and exec form (RUN ["cmd", "arg"]).
func getRunCommandString(run *instructions.RunCommand) string {
	// CmdLine contains the command parts for both shell and exec forms
	return strings.Join(run.CmdLine, " ")
}

// New creates a new DL3004 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
