package tally

import (
	"fmt"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// ShellRunInScratchRuleCode is the full rule code.
const ShellRunInScratchRuleCode = rules.TallyRulePrefix + "shell-run-in-scratch"

// ShellRunInScratchRule detects shell-form RUN instructions in scratch stages.
// scratch images contain no shell, so shell-form RUN will always fail.
// If the user has explicitly set a SHELL instruction, the warning is suppressed
// because the user likely bootstrapped a shell binary into the stage.
type ShellRunInScratchRule struct{}

// NewShellRunInScratchRule creates a new rule instance.
func NewShellRunInScratchRule() *ShellRunInScratchRule {
	return &ShellRunInScratchRule{}
}

// Metadata returns the rule metadata.
func (r *ShellRunInScratchRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            ShellRunInScratchRuleCode,
		Name:            "Shell-form RUN in Scratch Stage",
		Description:     "Detects shell-form RUN instructions in scratch stages where no shell exists",
		DocURL:          rules.TallyDocURL(ShellRunInScratchRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the shell-run-in-scratch rule.
func (r *ShellRunInScratchRule) Check(input rules.LintInput) []rules.Violation {
	if input.Semantic == nil {
		return nil
	}

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	var violations []rules.Violation

	for i := range sem.StageCount() {
		info := sem.StageInfo(i)
		if info == nil || !info.IsScratch() {
			continue
		}

		stageName := formatScratchRunStageName(info, i)

		// Track whether a SHELL instruction has been seen. Only suppress
		// shell-form RUN warnings that appear *after* an explicit SHELL,
		// since earlier RUNs still execute with the default (missing) shell.
		shellSeen := false

		for _, cmd := range info.Stage.Commands {
			if _, ok := cmd.(*instructions.ShellCommand); ok {
				shellSeen = true
				continue
			}

			run, ok := cmd.(*instructions.RunCommand)
			if !ok || !run.PrependShell || shellSeen {
				continue
			}

			loc := rules.NewLocationFromRanges(input.File, run.Location())

			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				fmt.Sprintf("shell-form RUN in scratch %s will fail because scratch has no /bin/sh", stageName),
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL).WithDetail(
				"scratch images contain no shell. Use exec-form (RUN [\"/binary\", \"arg\"]), "+
					"change the base image, or add a SHELL instruction if you've bootstrapped a shell",
			))
		}
	}

	return violations
}

// formatScratchRunStageName formats a stage reference for error messages.
func formatScratchRunStageName(info *semantic.StageInfo, idx int) string {
	if info != nil && info.Stage != nil && info.Stage.Name != "" {
		return fmt.Sprintf("%q (stage %d)", info.Stage.Name, idx)
	}
	return fmt.Sprintf("stage %d", idx)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewShellRunInScratchRule())
}
