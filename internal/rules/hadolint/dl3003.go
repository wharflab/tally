package hadolint

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/dockerfile"
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

	return ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			cmdStr := dockerfile.RunCommandString(run)

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
			if fix := r.generateFix(input, run, cdCommands, file, shellVariant); fix != nil {
				v = v.WithSuggestedFix(fix)
			}

			return []rules.Violation{v}
		},
	)
}

// generateFix attempts to generate an auto-fix for cd usage.
// Handles all cd commands in a chain by converting each to WORKDIR.
// Example: "RUN cd /tmp && git clone ... && cd repo && make"
// becomes: "WORKDIR /tmp\nRUN git clone ...\nWORKDIR repo\nRUN make"
//
// If prefer-run-heredoc is enabled and this command is a heredoc candidate,
// we skip generating a fix because splitting would interfere with heredoc conversion.
// The cd inside a heredoc works correctly (affects subsequent commands in same RUN).
func (r *DL3003Rule) generateFix(
	input rules.LintInput,
	run *instructions.RunCommand,
	cdCommands []shell.CdCommand,
	file string,
	shellVariant shell.Variant,
) *rules.SuggestedFix {
	// Only handle shell form RUN commands
	if !run.PrependShell {
		return nil
	}

	// If prefer-run-heredoc is enabled and this command is a heredoc candidate,
	// skip the fix - heredoc conversion handles cd correctly and is preferable
	// to splitting the RUN into multiple instructions.
	if input.IsRuleEnabled(rules.HeredocRuleCode) {
		cmdStr := dockerfile.RunCommandString(run)
		if shell.IsHeredocCandidate(cmdStr, shellVariant, input.GetHeredocMinCommands()) {
			return nil
		}
	}

	// Filter cd commands with valid target directories
	var validCds []shell.CdCommand
	for _, cd := range cdCommands {
		if cd.TargetDir != "" {
			validCds = append(validCds, cd)
		}
	}

	if len(validCds) == 0 {
		return nil
	}

	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	// Calculate the actual end position for the edit
	lastRange := runLoc[len(runLoc)-1]
	endLine := lastRange.End.Line
	endCol := lastRange.End.Character

	if endLine == runLoc[0].Start.Line && endCol == runLoc[0].Start.Character {
		cmdStr := dockerfile.RunCommandString(run)
		fullInstr := "RUN " + cmdStr
		endCol = runLoc[0].Start.Character + len(fullInstr)
	}

	// Build the replacement text by processing all cd commands
	newText := buildMultiCdFix(validCds, shellVariant)
	if newText == "" {
		return nil
	}

	return &rules.SuggestedFix{
		Description: "Convert cd commands to WORKDIR instructions",
		Safety:      rules.FixSuggestion,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(
				file,
				runLoc[0].Start.Line,
				runLoc[0].Start.Character,
				endLine,
				endCol,
			),
			NewText: newText,
		}},
	}
}

// buildMultiCdFix builds a replacement string for a RUN command with multiple cd commands.
// Each cd becomes a WORKDIR, and commands between cds become RUN instructions.
// Redundant mkdir commands (mkdir <target> before cd <target>) are removed since WORKDIR creates dirs.
func buildMultiCdFix(cds []shell.CdCommand, variant shell.Variant) string {
	if len(cds) == 0 {
		return ""
	}

	var parts []string

	// Handle the first cd
	first := cds[0]

	// If there are commands before the first cd, emit them as RUN
	// Remove redundant mkdir that matches the cd target
	if first.PrecedingCommands != "" {
		preceding := removeRedundantMkdir(first.PrecedingCommands, first.TargetDir)
		if preceding != "" {
			parts = append(parts, "RUN "+preceding)
		}
	}

	// Emit WORKDIR for the first cd
	parts = append(parts, "WORKDIR "+first.TargetDir)

	// For each subsequent cd, we need to figure out what commands are between them
	// The RemainingCommands of cd[i] contains everything after it, including cd[i+1]
	// We need to extract just the commands between cd[i] and cd[i+1]

	// Handle first cd's remaining commands (WORKDIR already emitted above)
	if len(cds) > 1 {
		if between := commandsBetweenCds(first, cds[1], variant); between != "" {
			parts = append(parts, "RUN "+between)
		}
	} else if first.RemainingCommands != "" {
		parts = append(parts, "RUN "+first.RemainingCommands)
	}

	// Handle subsequent cds
	for i := 1; i < len(cds); i++ {
		cd := cds[i]
		parts = append(parts, "WORKDIR "+cd.TargetDir)

		if i < len(cds)-1 {
			if between := commandsBetweenCds(cd, cds[i+1], variant); between != "" {
				parts = append(parts, "RUN "+between)
			}
		} else if cd.RemainingCommands != "" {
			parts = append(parts, "RUN "+cd.RemainingCommands)
		}
	}

	return strings.Join(parts, "\n")
}

// commandsBetweenCds extracts and cleans commands between two consecutive cd commands.
func commandsBetweenCds(current, next shell.CdCommand, variant shell.Variant) string {
	cmdsBetween := shell.ExtractCommandsBetweenCds(current.RemainingCommands, variant)
	return removeRedundantMkdir(cmdsBetween, next.TargetDir)
}

// removeRedundantMkdir removes "mkdir <target>" from commands since WORKDIR creates the directory.
// Handles patterns like "mkdir /tmp/foo" or "mkdir -p /tmp/foo" at start, middle, or end of chain.
func removeRedundantMkdir(commands, target string) string {
	if commands == "" || target == "" {
		return commands
	}

	// Patterns to remove: "mkdir <target>", "mkdir -p <target>", etc.
	patterns := []string{
		"mkdir -p " + target,
		"mkdir " + target,
	}

	result := commands
	for _, pattern := range patterns {
		// Check if pattern is the entire command
		if result == pattern {
			return ""
		}

		// Check if pattern is at the start: "mkdir /tmp && ..."
		if after, ok := strings.CutPrefix(result, pattern+" && "); ok {
			result = after
			continue
		}
		if after, ok := strings.CutPrefix(result, pattern+" &&"); ok {
			result = after
			result = strings.TrimSpace(result)
			continue
		}

		// Check if pattern is in the middle: "... && mkdir /tmp && ..."
		if strings.Contains(result, " && "+pattern+" && ") {
			result = strings.Replace(result, " && "+pattern+" && ", " && ", 1)
			continue
		}

		// Check if pattern is at the end: "... && mkdir /tmp"
		if before, ok := strings.CutSuffix(result, " && "+pattern); ok {
			result = before
			continue
		}
	}

	return strings.TrimSpace(result)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3003Rule())
}
