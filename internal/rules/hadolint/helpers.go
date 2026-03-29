package hadolint

import (
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/runcheck"
)

// RunCommandCallback is kept as a compatibility alias for shared RUN scanners.
type RunCommandCallback = runcheck.RunCommandCallback

// ScanRunCommandsWithPOSIXShell delegates to the shared RUN-check helper.
func ScanRunCommandsWithPOSIXShell(input rules.LintInput, callback RunCommandCallback) []rules.Violation {
	return runcheck.ScanRunCommandsWithPOSIXShell(input, callback)
}

// PackageManagerRuleConfig is the shared command-flag config used by Hadolint
// package-manager rules.
type PackageManagerRuleConfig = runcheck.CommandFlagRuleConfig

// CheckPackageManagerFlag delegates to the shared command-flag checker.
func CheckPackageManagerFlag(input rules.LintInput, meta rules.RuleMetadata, config PackageManagerRuleConfig) []rules.Violation {
	return runcheck.CheckCommandFlag(input, meta, config)
}
