package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
)

// DL3003Rule implements the DL3003 linting rule.
type DL3003Rule struct{}

// NewDL3003Rule creates a new DL3003 rule instance.
func NewDL3003Rule() *DL3003Rule {
	return &DL3003Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3003Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3003",
		Name:            "Use WORKDIR to switch to a directory",
		Description:     "Use WORKDIR to switch to a directory instead of using cd in RUN commands",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3003",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "style",
		IsExperimental:  false,
	}
}

// Check runs the DL3003 rule.
// It warns when any RUN instruction contains a cd command.
// Skips analysis for stages using non-POSIX shells (e.g., PowerShell).
func (r *DL3003Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	return ScanRunCommandsWithPOSIXShell(input, func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
		cmdStr := GetRunCommandString(run)

		// Check if the command contains cd (handles subshells, etc.)
		if !shell.ContainsCommandWithVariant(cmdStr, "cd", shellVariant) {
			return nil
		}

		loc := rules.NewLocationFromRanges(file, run.Location())

		v := rules.NewViolation(
			loc,
			meta.Code,
			"use WORKDIR to switch to a directory",
			meta.DefaultSeverity,
		).WithDocURL(meta.DocURL).WithDetail(
			"The cd command in a RUN instruction only affects that single instruction. " +
				"Use WORKDIR to set the working directory persistently for subsequent instructions. " +
				"Alternatively, use absolute paths in your commands.",
		)

		// Try to generate auto-fix for simple cases
		// Use FindCdCommands for detailed analysis (may not find cd in complex structures)
		cdCommands := shell.FindCdCommands(cmdStr, shellVariant)
		if fix := r.generateFix(run, cdCommands, file); fix != nil {
			v = v.WithSuggestedFix(fix)
		}

		return []rules.Violation{v}
	})
}

// generateFix attempts to generate an auto-fix for cd usage.
// Only handles simple cases:
// 1. Standalone "cd /path" -> Replace with "WORKDIR /path"
// 2. "cd /path && cmd" at start of chain -> Insert "WORKDIR /path" before, modify RUN
func (r *DL3003Rule) generateFix(run *instructions.RunCommand, cdCommands []shell.CdCommand, file string) *rules.SuggestedFix {
	// Only handle shell form RUN commands
	if !run.PrependShell {
		return nil
	}

	// Only handle single cd command cases for now
	if len(cdCommands) != 1 {
		return nil
	}

	cd := cdCommands[0]

	// Must have a target directory we can extract
	if cd.TargetDir == "" {
		return nil
	}

	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	// Case 1: Standalone cd - replace entire RUN with WORKDIR
	if cd.IsStandalone {
		return &rules.SuggestedFix{
			Description: "Replace RUN cd with WORKDIR " + cd.TargetDir,
			Safety:      rules.FixSafe,
			Edits: []rules.TextEdit{{
				Location: rules.NewRangeLocation(
					file,
					runLoc[0].Start.Line, // 1-based (BuildKit convention)
					runLoc[0].Start.Character,
					runLoc[0].End.Line,
					runLoc[0].End.Character,
				),
				NewText: "WORKDIR " + cd.TargetDir,
			}},
		}
	}

	// Case 2: cd at start with chained commands
	// "RUN cd /path && cmd" -> "WORKDIR /path\nRUN cmd"
	if cd.IsAtStart && cd.RemainingCommands != "" {
		return &rules.SuggestedFix{
			Description: "Split into WORKDIR " + cd.TargetDir + " and RUN " + cd.RemainingCommands,
			Safety:      rules.FixSuggestion, // May change behavior if cd was intentional
			Edits: []rules.TextEdit{{
				Location: rules.NewRangeLocation(
					file,
					runLoc[0].Start.Line, // 1-based (BuildKit convention)
					runLoc[0].Start.Character,
					runLoc[0].End.Line,
					runLoc[0].End.Character,
				),
				NewText: "WORKDIR " + cd.TargetDir + "\nRUN " + cd.RemainingCommands,
			}},
		}
	}

	return nil
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3003Rule())
}
