package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/shell"
)

// RunCommandCallback is a function that checks a RUN command with the given shell variant.
// It returns violations found for that specific RUN command.
type RunCommandCallback func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation

// ScanRunCommandsWithPOSIXShell scans all RUN instructions in the input,
// calling the provided callback for each RUN command with the appropriate shell variant.
// It skips non-POSIX shells (e.g., PowerShell, cmd) as they have incompatible syntax.
//
// This helper extracts the common pattern used by rules that need to check RUN commands
// with shell-aware parsing (e.g., DL3004, DL3027).
func ScanRunCommandsWithPOSIXShell(input rules.LintInput, callback RunCommandCallback) []rules.Violation {
	var violations []rules.Violation

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

			// Call the callback for each RUN command
			violations = append(violations, callback(run, shellVariant, input.File)...)
		}
	}

	return violations
}

// PackageManagerRuleConfig defines configuration for package manager flag rules.
// These rules check that install commands include non-interactive flags.
type PackageManagerRuleConfig struct {
	// CommandNames are the package manager commands to check (e.g., "apt-get", "dnf", "microdnf")
	CommandNames []string
	// Subcommands that require the non-interactive flag (e.g., "install", "groupinstall")
	Subcommands []string
	// HasRequiredFlag checks if the command has a valid non-interactive flag.
	// Return true if the command is valid (has required flag), false if violation.
	HasRequiredFlag func(cmd *shell.CommandInfo) bool
	// FixFlag is the flag to insert for auto-fix (e.g., " -y", " -n")
	FixFlag string
	// FixDescription describes the auto-fix action
	FixDescription string
	// Detail provides additional context for the violation message
	Detail string
}

// CheckPackageManagerFlag implements the common pattern for package manager flag rules.
// It scans RUN commands for package manager invocations and checks for required flags.
func CheckPackageManagerFlag(input rules.LintInput, meta rules.RuleMetadata, config PackageManagerRuleConfig) []rules.Violation {
	sm := input.SourceMap()

	return ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			var cmds []shell.CommandInfo
			var runStartLine int

			if run.PrependShell {
				script, startLine := getRunSourceScript(run, sm)
				if script == "" {
					return nil
				}
				runStartLine = startLine
				cmds = shell.FindCommands(script, shellVariant, config.CommandNames...)
			} else {
				cmdStr := dockerfile.RunCommandString(run)
				cmds = shell.FindCommands(cmdStr, shellVariant, config.CommandNames...)
			}

			var violations []rules.Violation
			for _, cmd := range cmds {
				// Check if this is a subcommand that requires the flag
				if !cmd.HasAnyArg(config.Subcommands...) {
					continue
				}

				// Check if the command has the required flag
				if config.HasRequiredFlag(&cmd) {
					continue
				}

				loc := rules.NewLocationFromRanges(file, run.Location())
				v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
					WithDocURL(meta.DocURL).
					WithDetail(config.Detail)

				// Add auto-fix for shell form RUN commands
				if run.PrependShell && cmd.Subcommand != "" {
					editLine := runStartLine + cmd.SubcommandLine
					insertCol := cmd.SubcommandEndCol

					lineIdx := editLine - 1
					if lineIdx >= 0 && lineIdx < sm.LineCount() {
						sourceLine := sm.Line(lineIdx)
						if insertCol >= 0 && insertCol <= len(sourceLine) {
							v = v.WithSuggestedFix(&rules.SuggestedFix{
								Description: config.FixDescription,
								Safety:      rules.FixSafe,
								Edits: []rules.TextEdit{{
									Location: rules.NewRangeLocation(file, editLine, insertCol, editLine, insertCol),
									NewText:  config.FixFlag,
								}},
							})
						}
					}
				}

				violations = append(violations, v)
			}

			return violations
		},
	)
}
