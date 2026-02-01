package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
)

// DL3027Rule implements the DL3027 linting rule.
type DL3027Rule struct{}

// NewDL3027Rule creates a new DL3027 rule instance.
func NewDL3027Rule() *DL3027Rule {
	return &DL3027Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3027Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3027",
		Name:            "Do not use apt",
		Description:     "Do not use apt as it is meant to be an end-user tool, use apt-get or apt-cache instead",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3027",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "style",
		IsExperimental:  false,
	}
}

// Check runs the DL3027 rule.
// It warns when any RUN instruction contains an apt command.
// Skips analysis for stages using non-POSIX shells (e.g., PowerShell).
func (r *DL3027Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	return ScanRunCommandsWithPOSIXShell(input, func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
		// Check if the command contains apt using the shell package
		cmdStr := GetRunCommandString(run)
		if shell.ContainsCommandWithVariant(cmdStr, "apt", shellVariant) {
			loc := rules.NewLocationFromRanges(file, run.Location())
			return []rules.Violation{
				rules.NewViolation(
					loc,
					meta.Code,
					"do not use apt as it is meant to be an end-user tool, use apt-get or apt-cache instead",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"The apt command is designed for interactive use and has an unstable command-line interface. "+
						"For scripting and automation (like Dockerfiles), use apt-get for package management "+
						"or apt-cache for querying package information.",
				),
			}
		}
		return nil
	})
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3027Rule())
}
