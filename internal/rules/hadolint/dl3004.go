package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
)

// DL3004Rule implements the DL3004 linting rule.
type DL3004Rule struct{}

// NewDL3004Rule creates a new DL3004 rule instance.
func NewDL3004Rule() *DL3004Rule {
	return &DL3004Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3004Rule) Metadata() rules.RuleMetadata {
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
func (r *DL3004Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	return ScanRunCommandsWithPOSIXShell(input, func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
		// Check if the command contains sudo using the shell package
		cmdStr := GetRunCommandString(run)
		if shell.ContainsCommandWithVariant(cmdStr, "sudo", shellVariant) {
			loc := rules.NewLocationFromRanges(file, run.Location())
			return []rules.Violation{
				rules.NewViolation(
					loc,
					meta.Code,
					"do not use sudo in RUN commands; it has unpredictable TTY and signal handling",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"sudo is designed for interactive use and doesn't work reliably in containers. "+
						"Instead, use the USER instruction to switch users, or run specific commands "+
						"as a different user with 'su -c' if necessary.",
				),
			}
		}
		return nil
	})
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3004Rule())
}
