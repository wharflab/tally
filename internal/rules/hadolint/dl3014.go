package hadolint

import (
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
)

// DL3014Rule implements the DL3014 linting rule.
// It warns when apt-get install commands don't use the -y switch.
type DL3014Rule struct{}

// NewDL3014Rule creates a new DL3014 rule instance.
func NewDL3014Rule() *DL3014Rule {
	return &DL3014Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3014Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3014",
		Name:            "Use -y with apt-get install",
		Description:     "Use the -y switch to avoid manual input `apt-get -y install <package>`",
		DocURL:          rules.HadolintDocURL("DL3014"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// Check runs the DL3014 rule.
func (r *DL3014Rule) Check(input rules.LintInput) []rules.Violation {
	return CheckPackageManagerFlag(input, r.Metadata(), PackageManagerRuleConfig{
		CommandNames:    []string{"apt-get"},
		Subcommands:     []string{"install"},
		HasRequiredFlag: hasAptGetYesOption,
		FixFlag:         " -y",
		FixDescription:  "Add -y flag to apt-get install",
		Detail: "Running apt-get install without -y will cause the command to wait for " +
			"user confirmation, which will hang in Docker builds. Use -y, --yes, " +
			"--assume-yes, or -qq to automatically confirm.",
	})
}

// hasAptGetYesOption checks if an apt-get command has a "yes" option.
// Per Hadolint DL3014, these are: -y, --yes, -qq, --assume-yes, -q -q, --quiet --quiet, -q=2
func hasAptGetYesOption(cmd *shell.CommandInfo) bool {
	// Check direct flags
	if cmd.HasAnyFlag("-y", "--yes", "-qq", "--assume-yes") {
		return true
	}

	// Check for -q count >= 2 (either -q -q or combined in other args)
	if cmd.CountFlag("-q") >= 2 {
		return true
	}

	// Check for --quiet count >= 2
	if cmd.CountFlag("--quiet") >= 2 {
		return true
	}

	// Check for -q=2
	if cmd.GetArgValue("-q") == "2" {
		return true
	}

	return false
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3014Rule())
}
