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
// For non-POSIX shells (e.g., PowerShell, cmd), it skips shell-form RUNs but still
// processes exec-form RUNs since they don't require shell parsing.
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
		isNonPOSIX := false
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				isNonPOSIX = shellVariant.IsNonPOSIX()
			}
		}

		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}

			// For non-POSIX shells, skip shell-form RUNs (they need shell parsing)
			// but still process exec-form RUNs (PrependShell=false) since they
			// execute binaries directly without shell interpretation.
			if isNonPOSIX && run.PrependShell {
				continue
			}

			// For exec-form in non-POSIX stages, use default bash variant for parsing
			// the command string (exec-form arguments don't need shell parsing anyway)
			effectiveVariant := shellVariant
			if isNonPOSIX {
				effectiveVariant = shell.VariantBash
			}

			// Call the callback for each RUN command
			violations = append(violations, callback(run, effectiveVariant, input.File)...)
		}

		// Also check ONBUILD RUN commands from the semantic model.
		// The parsed commands have Location() patched to the original ONBUILD
		// source line, so callbacks can use source-map lookup and generate
		// auto-fix edits with correct positions.
		if sem != nil {
			for _, onbuild := range sem.OnbuildInstructions(stageIdx) {
				run, ok := onbuild.Command.(*instructions.RunCommand)
				if !ok {
					continue
				}

				if isNonPOSIX && run.PrependShell {
					continue
				}

				effectiveVariant := shellVariant
				if isNonPOSIX {
					effectiveVariant = shell.VariantBash
				}

				violations = append(violations, callback(run, effectiveVariant, input.File)...)
			}
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
