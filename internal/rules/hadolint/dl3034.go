package hadolint

import (
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
)

// DL3034Rule implements the DL3034 linting rule.
// It warns when zypper install/in/remove/rm/patch commands don't use non-interactive flags.
type DL3034Rule struct{}

// NewDL3034Rule creates a new DL3034 rule instance.
func NewDL3034Rule() *DL3034Rule {
	return &DL3034Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3034Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3034",
		Name:            "Use non-interactive with zypper",
		Description:     "Non-interactive switch missing from `zypper` command: `zypper install -y`",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3034",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// zypperSubcommands are the zypper subcommands that require non-interactive flags.
// Includes both full names and aliases.
var zypperSubcommands = []string{"install", "in", "remove", "rm", "patch", "source-install", "si"}

// Check runs the DL3034 rule.
func (r *DL3034Rule) Check(input rules.LintInput) []rules.Violation {
	return CheckPackageManagerFlag(input, r.Metadata(), PackageManagerRuleConfig{
		CommandNames: []string{"zypper"},
		Subcommands:  zypperSubcommands,
		HasRequiredFlag: func(cmd *shell.CommandInfo) bool {
			return cmd.HasAnyFlag("-n", "--non-interactive", "-y", "--no-confirm")
		},
		FixFlag:        " -n",
		FixDescription: "Add -n flag to zypper command",
		Detail: "Running zypper install without -n will cause the command to wait for " +
			"user confirmation, which will hang in Docker builds. Use -n, " +
			"--non-interactive, -y, or --no-confirm to automatically confirm.",
	})
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3034Rule())
}
