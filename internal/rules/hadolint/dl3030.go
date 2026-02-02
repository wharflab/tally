package hadolint

import (
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
)

// DL3030Rule implements the DL3030 linting rule.
// It warns when yum install/groupinstall/localinstall commands don't use the -y switch.
type DL3030Rule struct{}

// NewDL3030Rule creates a new DL3030 rule instance.
func NewDL3030Rule() *DL3030Rule {
	return &DL3030Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3030Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3030",
		Name:            "Use -y with yum install",
		Description:     "Use the -y switch to avoid manual input `yum install -y <package>`",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3030",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// yumInstallSubcommands are the yum subcommands that require -y flag.
var yumInstallSubcommands = []string{"install", "groupinstall", "localinstall", "reinstall"}

// Check runs the DL3030 rule.
func (r *DL3030Rule) Check(input rules.LintInput) []rules.Violation {
	return CheckPackageManagerFlag(input, r.Metadata(), PackageManagerRuleConfig{
		CommandNames:    []string{"yum"},
		Subcommands:     yumInstallSubcommands,
		HasRequiredFlag: func(cmd *shell.CommandInfo) bool { return cmd.HasAnyFlag("-y", "--assumeyes") },
		FixFlag:         " -y",
		FixDescription:  "Add -y flag to yum install",
		Detail: "Running yum install without -y will cause the command to wait for " +
			"user confirmation, which will hang in Docker builds. Use -y or " +
			"--assumeyes to automatically confirm.",
	})
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3030Rule())
}
