package hadolint

import (
	"fmt"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/shell"
)

// DL4005Rule implements the DL4005 linting rule.
type DL4005Rule struct{}

// NewDL4005Rule creates a new DL4005 rule instance.
func NewDL4005Rule() *DL4005Rule {
	return &DL4005Rule{}
}

// Metadata returns the rule metadata.
func (r *DL4005Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL4005",
		Name:            "Use SHELL to change the default shell",
		Description:     "Use SHELL to change the default shell",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL4005",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "style",
		IsExperimental:  false,
	}
}

// Check runs the DL4005 rule.
// It warns when a RUN instruction uses ln to symlink /bin/sh, which is a
// fragile way to change the default shell. The SHELL instruction should be
// used instead.
func (r *DL4005Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	return ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			cmdStr := dockerfile.RunCommandString(run)
			lnCmds := shell.FindCommands(cmdStr, shellVariant, "ln")
			for _, cmd := range lnCmds {
				if !slices.Contains(cmd.Args, "/bin/sh") {
					continue
				}

				loc := rules.NewLocationFromRanges(file, run.Location())
				v := rules.NewViolation(
					loc,
					meta.Code,
					"use SHELL to change the default shell instead of ln to /bin/sh",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"Using ln to redirect /bin/sh is fragile and may break scripts that depend on " +
						"the original shell. Use the SHELL instruction to change the default shell " +
						"for subsequent RUN instructions, e.g., SHELL [\"/bin/bash\", \"-c\"].",
				)

				if fix := r.generateFix(run, cmdStr, shellVariant, cmd, file); fix != nil {
					v = v.WithSuggestedFix(fix)
				}

				return []rules.Violation{v}
			}
			return nil
		},
	)
}

// generateFix builds a suggested fix that replaces the ln command with a
// SHELL instruction.  When ln is part of a && chain, the remaining commands
// are preserved as a RUN instruction.
func (r *DL4005Rule) generateFix(
	run *instructions.RunCommand,
	cmdStr string,
	shellVariant shell.Variant,
	cmd shell.CommandInfo,
	file string,
) *rules.SuggestedFix {
	// Only handle shell form RUN commands.
	if !run.PrependShell {
		return nil
	}

	// Determine the target shell from the ln arguments.
	// In "ln -sf /bin/bash /bin/sh", Subcommand is "/bin/bash".
	targetShell := cmd.Subcommand
	if targetShell == "" || targetShell == "/bin/sh" {
		return nil
	}

	// Find where the ln command sits in the chain.
	pos := shell.FindCommandInChain(cmdStr, shellVariant, func(name string, args []string) bool {
		return name == "ln" && slices.Contains(args, "/bin/sh")
	})
	if pos == nil {
		return nil
	}

	// When semicolons separate multiple top-level statements, the chain
	// context only covers the statement containing the match. Emitting a
	// fix here would silently drop commands from sibling statements.
	if pos.HasOtherStatements {
		return nil
	}

	shellInstr := fmt.Sprintf(`SHELL [%q, "-c"]`, targetShell)

	var parts []string
	if pos.PrecedingCommands != "" {
		parts = append(parts, "RUN "+pos.PrecedingCommands)
	}
	parts = append(parts, shellInstr)
	if pos.RemainingCommands != "" {
		parts = append(parts, "RUN "+pos.RemainingCommands)
	}
	newText := strings.Join(parts, "\n")

	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	lastRange := runLoc[len(runLoc)-1]
	endLine := lastRange.End.Line
	endCol := lastRange.End.Character

	// BuildKit sometimes returns a zero-width range (start == end).
	// Compute the actual end column from the instruction text.
	if endLine == runLoc[0].Start.Line && endCol == runLoc[0].Start.Character {
		fullInstr := "RUN " + cmdStr
		endCol = runLoc[0].Start.Character + len(fullInstr)
	}

	return &rules.SuggestedFix{
		Description: "Replace ln /bin/sh with SHELL instruction",
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

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL4005Rule())
}
