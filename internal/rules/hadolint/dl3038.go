package hadolint

import (
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
)

// DL3038Rule implements the DL3038 linting rule.
// It warns when dnf/microdnf install commands don't use the -y switch.
type DL3038Rule struct{}

// NewDL3038Rule creates a new DL3038 rule instance.
func NewDL3038Rule() *DL3038Rule {
	return &DL3038Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3038Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3038",
		Name:            "Use -y with dnf install",
		Description:     "Use the -y switch to avoid manual input `dnf install -y <package>`",
		DocURL:          rules.HadolintDocURL("DL3038"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// dnfInstallSubcommands are the dnf subcommands that require -y flag.
var dnfInstallSubcommands = []string{"install", "groupinstall", "localinstall"}

// Check runs the DL3038 rule.
func (r *DL3038Rule) Check(input rules.LintInput) []rules.Violation {
	return CheckPackageManagerFlag(input, r.Metadata(), PackageManagerRuleConfig{
		CommandNames:    []string{"dnf", "microdnf"},
		Subcommands:     dnfInstallSubcommands,
		HasRequiredFlag: func(cmd *shell.CommandInfo) bool { return cmd.HasAnyFlag("-y", "--assumeyes") },
		FixFlag:         " -y",
		FixDescription:  "Add -y flag to dnf install",
		Detail: "Running dnf/microdnf install without -y will cause the command to wait for " +
			"user confirmation, which will hang in Docker builds. Use -y or " +
			"--assumeyes to automatically confirm.",
	})
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3038Rule())
}
