// Package tally implements tally-specific linting rules for Dockerfiles.
package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
	"github.com/tinovyatkin/tally/internal/runmount"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/shell"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

// PreferHeredocConfig is the configuration for the prefer-run-heredoc rule.
type PreferHeredocConfig struct {
	// MinCommands is the minimum number of commands to trigger the rule.
	// Heredocs add 2 lines overhead (<<EOF and EOF), so default is 3.
	MinCommands *int `json:"min-commands,omitempty" koanf:"min-commands"`

	// CheckConsecutiveRuns enables detection of multiple consecutive RUN instructions.
	CheckConsecutiveRuns *bool `json:"check-consecutive-runs,omitempty" koanf:"check-consecutive-runs"`

	// CheckChainedCommands enables detection of chained commands via &&.
	CheckChainedCommands *bool `json:"check-chained-commands,omitempty" koanf:"check-chained-commands"`
}

// DefaultPreferHeredocConfig returns the default configuration.
func DefaultPreferHeredocConfig() PreferHeredocConfig {
	minCommands := 3
	checkConsecutive := true
	checkChained := true
	return PreferHeredocConfig{
		MinCommands:          &minCommands,
		CheckConsecutiveRuns: &checkConsecutive,
		CheckChainedCommands: &checkChained,
	}
}

// PreferHeredocRule implements the prefer-run-heredoc linting rule.
type PreferHeredocRule struct{}

// NewPreferHeredocRule creates a new prefer-run-heredoc rule instance.
func NewPreferHeredocRule() *PreferHeredocRule {
	return &PreferHeredocRule{}
}

// Metadata returns the rule metadata.
func (r *PreferHeredocRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.TallyRulePrefix + "prefer-run-heredoc",
		Name:            "Prefer RUN heredoc syntax",
		Description:     "Use heredoc syntax for multi-command RUN instructions",
		DocURL:          "https://github.com/tinovyatkin/tally/blob/main/docs/rules/prefer-run-heredoc.md",
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  true,
		FixPriority:     100, // Structural transform: run after content fixes
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferHeredocRule) Schema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"min-commands": map[string]any{
				"type":        "integer",
				"minimum":     2,
				"default":     3,
				"description": "Minimum commands to suggest heredoc",
			},
			"check-consecutive-runs": map[string]any{
				"type":        "boolean",
				"default":     true,
				"description": "Check for consecutive RUN instructions",
			},
			"check-chained-commands": map[string]any{
				"type":        "boolean",
				"default":     true,
				"description": "Check for chained commands in single RUN",
			},
		},
		"additionalProperties": false,
	}
}

// Check runs the prefer-run-heredoc rule.
func (r *PreferHeredocRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)

	// Get effective minCommands (default 3)
	minCommands := 3
	if cfg.MinCommands != nil {
		minCommands = *cfg.MinCommands
	}
	if minCommands < 2 {
		minCommands = 2
	}

	checkConsecutive := cfg.CheckConsecutiveRuns == nil || *cfg.CheckConsecutiveRuns
	checkChained := cfg.CheckChainedCommands == nil || *cfg.CheckChainedCommands

	var violations []rules.Violation
	meta := r.Metadata()
	sm := input.SourceMap()

	// Get semantic model for shell variant info (may be nil)
	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Type assertion OK returns false for nil, sem is nil-checked below

	for stageIdx, stage := range input.Stages {
		// Get shell variant for this stage
		shellVariant := shell.VariantBash
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				if shellVariant.IsNonPOSIX() {
					continue
				}
			}
		}

		// Check consecutive RUNs
		if checkConsecutive {
			violations = append(violations,
				r.checkConsecutiveRuns(stage, stageIdx, shellVariant, input.File, sm, minCommands, meta)...)
		}

		// Check chained commands within single RUN
		if checkChained {
			violations = append(violations,
				r.checkChainedCommands(stage, stageIdx, shellVariant, input.File, sm, minCommands, meta)...)
		}
	}

	return violations
}

// DefaultConfig returns the default configuration for this rule.
func (r *PreferHeredocRule) DefaultConfig() any {
	return DefaultPreferHeredocConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferHeredocRule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

// runSequenceItem represents a RUN instruction in a sequence with extracted commands.
type runSequenceItem struct {
	run      *instructions.RunCommand
	commands []string
	isSimple bool // true if all commands can be merged
}

// checkConsecutiveRuns checks for sequences of consecutive RUN instructions.
func (r *PreferHeredocRule) checkConsecutiveRuns(
	stage instructions.Stage,
	stageIdx int,
	shellVariant shell.Variant,
	file string,
	sm *sourcemap.SourceMap,
	minCommands int,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	sequence := make([]runSequenceItem, 0, 8) //nolint:mnd // Pre-allocate for typical sequences
	var sequenceMounts []*instructions.Mount

	flushSequence := func() {
		if v := r.createSequenceViolation(sequence, stageIdx, shellVariant, file, sm, minCommands, meta); v != nil {
			violations = append(violations, *v)
		}
		sequence = sequence[:0]
	}

	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			flushSequence()
			sequenceMounts = nil
			continue
		}

		// Check if mounts are compatible with current sequence
		runMounts := runmount.GetMounts(run)
		if len(sequence) > 0 && !runmount.MountsEqual(sequenceMounts, runMounts) {
			flushSequence()
			sequenceMounts = nil
		}

		// Extract commands from this RUN
		commands, isSimple := r.extractRunCommands(run, shellVariant)
		if len(commands) == 0 {
			flushSequence()
			sequenceMounts = nil
			continue
		}

		// Check if any command has exit (breaks sequence)
		script := getRunScriptFromCmd(run)
		if shell.HasExitCommand(script, shellVariant) {
			flushSequence()
			sequenceMounts = nil
			continue
		}

		// Start or continue sequence
		if len(sequence) == 0 {
			sequenceMounts = runMounts
		}

		sequence = append(sequence, runSequenceItem{
			run:      run,
			commands: commands,
			isSimple: isSimple,
		})
	}

	flushSequence()
	return violations
}

// createSequenceViolation creates a violation for a sequence of consecutive RUN instructions.
// Returns nil if the sequence doesn't warrant a violation.
func (r *PreferHeredocRule) createSequenceViolation(
	sequence []runSequenceItem,
	stageIdx int,
	shellVariant shell.Variant,
	file string,
	_ *sourcemap.SourceMap,
	minCommands int,
	meta rules.RuleMetadata,
) *rules.Violation {
	if len(sequence) < 2 {
		return nil
	}

	// Count total commands in sequence
	totalCommands := 0
	allSimple := true
	var allCommands []string
	for _, item := range sequence {
		totalCommands += len(item.commands)
		allCommands = append(allCommands, item.commands...)
		if !item.isSimple {
			allSimple = false
		}
	}

	if totalCommands < minCommands {
		return nil
	}

	firstRun := sequence[0].run
	loc := rules.NewLocationFromRanges(file, firstRun.Location())

	var detail string
	if allSimple {
		detail = fmt.Sprintf(
			"%d consecutive RUN instructions with %d total commands can be combined into a single heredoc RUN",
			len(sequence), totalCommands,
		)
	} else {
		detail = fmt.Sprintf(
			"%d consecutive RUN instructions with %d total commands (some contain complex logic)",
			len(sequence), totalCommands,
		)
	}

	v := rules.NewViolation(
		loc,
		meta.Code,
		"consecutive RUN instructions can be combined using heredoc syntax",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(detail)

	// Generate async fix only if all commands are simple
	// Uses async resolution to operate on content after sync fixes are applied
	if allSimple && len(allCommands) > 0 {
		fix := r.generateConsecutiveAsyncFix(stageIdx, shellVariant, allCommands, minCommands, meta)
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

// checkChainedCommands checks for single RUN instructions with many chained commands.
func (r *PreferHeredocRule) checkChainedCommands(
	stage instructions.Stage,
	stageIdx int,
	shellVariant shell.Variant,
	file string,
	_ *sourcemap.SourceMap,
	minCommands int,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok {
			continue
		}

		// Only check shell form RUNs
		if !run.PrependShell {
			continue
		}

		// Skip heredoc RUNs (they already use the preferred syntax)
		if len(run.Files) > 0 {
			continue
		}

		script := getRunScriptFromCmd(run)
		if script == "" {
			continue
		}

		commandCount := shell.CountChainedCommands(script, shellVariant)
		if commandCount >= minCommands {
			loc := rules.NewLocationFromRanges(file, run.Location())

			v := rules.NewViolation(
				loc,
				meta.Code,
				"RUN instruction with chained commands can use heredoc syntax",
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL).WithDetail(
				fmt.Sprintf("RUN has %d chained commands; consider using heredoc syntax for better readability", commandCount),
			)

			// Generate async fix for simple scripts
			// Uses async resolution to operate on content after sync fixes are applied
			if shell.IsSimpleScript(script, shellVariant) {
				commands := shell.ExtractChainedCommands(script, shellVariant)
				if len(commands) > 0 {
					fix := r.generateChainedAsyncFix(stageIdx, shellVariant, commands, minCommands, meta)
					v = v.WithSuggestedFix(fix)
				}
			}

			violations = append(violations, v)
		}
	}

	return violations
}

// extractRunCommands extracts commands from a RUN instruction.
// Returns the list of commands and whether they are all simple (mergeable).
// For complex commands (if, for, while, etc.), returns a single-element list
// containing the whole script to allow counting for sequence detection.
func (r *PreferHeredocRule) extractRunCommands(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
) ([]string, bool) {
	if len(run.Files) > 0 {
		// Heredoc RUN - extract from heredoc content
		if run.Files[0].Data == "" {
			return nil, false
		}
		script := run.Files[0].Data
		if !shell.IsSimpleScript(script, shellVariant) {
			// Complex heredoc - treat as single opaque command
			return []string{script}, false
		}
		commands := shell.ExtractChainedCommands(script, shellVariant)
		return commands, true
	}

	// Regular RUN - parse command line
	script := getRunScriptFromCmd(run)
	if script == "" {
		return nil, false
	}

	if !shell.IsSimpleScript(script, shellVariant) {
		// Complex script - treat as single opaque command (no fix possible)
		return []string{script}, false
	}

	commands := shell.ExtractChainedCommands(script, shellVariant)
	return commands, true
}

// generateConsecutiveAsyncFix generates an async fix for consecutive RUN instructions.
// The fix is resolved at apply time to compute correct positions after sync fixes.
func (r *PreferHeredocRule) generateConsecutiveAsyncFix(
	stageIdx int,
	shellVariant shell.Variant,
	commands []string,
	minCommands int,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	return r.generateHeredocAsyncFix(
		rules.HeredocFixConsecutive,
		fmt.Sprintf("Combine %d commands into heredoc", len(commands)),
		stageIdx, shellVariant, minCommands, meta,
	)
}

// generateChainedAsyncFix generates an async fix for a single RUN with chained commands.
// The fix is resolved at apply time to compute correct positions after sync fixes.
func (r *PreferHeredocRule) generateChainedAsyncFix(
	stageIdx int,
	shellVariant shell.Variant,
	commands []string,
	minCommands int,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	return r.generateHeredocAsyncFix(
		rules.HeredocFixChained,
		fmt.Sprintf("Convert chained commands to heredoc (%d commands)", len(commands)),
		stageIdx, shellVariant, minCommands, meta,
	)
}

// generateHeredocAsyncFix is the common implementation for async heredoc fixes.
// The resolver uses re-parsing to find fixes rather than fingerprint matching,
// which is more robust when content changes due to sync fixes applied first.
func (r *PreferHeredocRule) generateHeredocAsyncFix(
	fixType rules.HeredocFixType,
	description string,
	stageIdx int,
	shellVariant shell.Variant,
	minCommands int,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	return &rules.SuggestedFix{
		Description:  description,
		Safety:       rules.FixSuggestion,
		Priority:     meta.FixPriority,
		NeedsResolve: true,
		ResolverID:   rules.HeredocResolverID,
		ResolverData: &rules.HeredocResolveData{
			Type:         fixType,
			StageIndex:   stageIdx,
			ShellVariant: shellVariant,
			MinCommands:  minCommands,
		},
	}
}

// resolveConfig extracts the PreferHeredocConfig from input, falling back to defaults.
func (r *PreferHeredocRule) resolveConfig(config any) PreferHeredocConfig {
	return configutil.Coerce(config, DefaultPreferHeredocConfig())
}

// getRunScriptFromCmd extracts the shell script from a RUN instruction.
// For heredoc RUNs, returns the heredoc content. For regular RUNs, returns CmdLine.
// This is important for detecting exit commands that would break merging semantics.
func getRunScriptFromCmd(run *instructions.RunCommand) string {
	// Prefer heredoc content when present - important for detecting exit commands
	if len(run.Files) > 0 && run.Files[0].Data != "" {
		return run.Files[0].Data
	}
	// Use the parsed CmdLine which has the actual command without mount options
	if len(run.CmdLine) > 0 {
		return strings.Join(run.CmdLine, " ")
	}
	return ""
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferHeredocRule())
}
