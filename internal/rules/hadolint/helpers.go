package hadolint

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

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

// GetRunCommandString extracts the command string from a RUN instruction.
// Handles both shell form (RUN cmd) and exec form (RUN ["cmd", "arg"]).
func GetRunCommandString(run *instructions.RunCommand) string {
	// CmdLine contains the command parts for both shell and exec forms
	return strings.Join(run.CmdLine, " ")
}
