// Package nosudo implements hadolint DL3004.
// This rule warns when sudo is used in RUN instructions,
// as it has unpredictable behavior in containers.
package nosudo

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Rule implements the DL3004 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3004",
		Name:            "Do not use sudo",
		Description:     "Do not use sudo as it has unpredictable behavior in containers",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3004",
		DefaultSeverity: rules.SeverityError,
		Category:        "security",
		IsExperimental:  false,
	}
}

// Check runs the DL3004 rule.
// It warns when any RUN instruction contains a sudo command.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation

	for _, stage := range input.Stages {
		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}

			// Check if the command contains sudo
			cmdStr := getRunCommandString(run)
			if containsSudo(cmdStr) {
				loc := rules.NewLocationFromRanges(input.File, run.Location())
				violations = append(violations, rules.NewViolation(
					loc,
					r.Metadata().Code,
					"do not use sudo in RUN commands; it has unpredictable TTY and signal handling",
					r.Metadata().DefaultSeverity,
				).WithDocURL(r.Metadata().DocURL).WithDetail(
					"sudo is designed for interactive use and doesn't work reliably in containers. "+
						"Instead, use the USER instruction to switch users, or run specific commands "+
						"as a different user with 'su -c' if necessary.",
				))
			}
		}
	}

	return violations
}

// getRunCommandString extracts the command string from a RUN instruction.
// Handles both shell form (RUN cmd) and exec form (RUN ["cmd", "arg"]).
func getRunCommandString(run *instructions.RunCommand) string {
	// CmdLine contains the command parts for both shell and exec forms
	return strings.Join(run.CmdLine, " ")
}

// containsSudo checks if a command string contains a sudo invocation.
// We look for sudo as a command (not just as a substring).
func containsSudo(cmd string) bool {
	// Normalize the command
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}

	// Check various patterns for sudo usage
	tokens := tokenizeShellCommand(cmd)
	return slices.Contains(tokens, "sudo")
}

// tokenizeShellCommand does basic shell tokenization to find command names.
// This handles common patterns like: sudo apt-get, apt-get && sudo something.
// It uses a simple heuristic: find tokens that appear in "command position"
// (first token in a sequence separated by shell operators).
func tokenizeShellCommand(cmd string) []string {
	var tokens []string

	// Replace common shell operators with a marker to split on
	// This is a simplification but catches most cases
	const marker = "\x00"
	for _, sep := range []string{"&&", "||", ";", "|", "`", "$("} {
		cmd = strings.ReplaceAll(cmd, sep, marker)
	}
	// Handle parentheses - they start new command contexts
	cmd = strings.ReplaceAll(cmd, "(", marker)
	cmd = strings.ReplaceAll(cmd, ")", " ")

	// Handle line continuations
	cmd = strings.ReplaceAll(cmd, "\\\n", " ")
	cmd = strings.ReplaceAll(cmd, "\n", marker)

	// Split by the marker to get individual command sequences
	sequences := strings.SplitSeq(cmd, marker)

	for seq := range sequences {
		seq = strings.TrimSpace(seq)
		if seq == "" {
			continue
		}

		// Get the first non-assignment, non-flag token as the command
		parts := strings.FieldsSeq(seq)
		for part := range parts {
			// Skip environment variable assignments (FOO=bar)
			if strings.Contains(part, "=") && !strings.HasPrefix(part, "-") {
				continue
			}
			// Skip flags
			if strings.HasPrefix(part, "-") {
				continue
			}
			// This is the command name
			tokens = append(tokens, part)
			break
		}
	}

	return tokens
}

// New creates a new DL3004 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
