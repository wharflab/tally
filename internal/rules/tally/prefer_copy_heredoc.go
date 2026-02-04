// Package tally implements tally-specific linting rules for Dockerfiles.
package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/shell"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

// PreferCopyHeredocRuleCode is the full rule code for the prefer-copy-heredoc rule.
const PreferCopyHeredocRuleCode = rules.TallyRulePrefix + "prefer-copy-heredoc"

// PreferCopyHeredocConfig is the configuration for the prefer-copy-heredoc rule.
type PreferCopyHeredocConfig struct {
	// CheckSingleRun enables detection of single RUN instructions with file creation.
	CheckSingleRun *bool `json:"check-single-run,omitempty" koanf:"check-single-run"`

	// CheckConsecutiveRuns enables detection of multiple consecutive RUN instructions
	// that create/append to the same file.
	CheckConsecutiveRuns *bool `json:"check-consecutive-runs,omitempty" koanf:"check-consecutive-runs"`
}

// DefaultPreferCopyHeredocConfig returns the default configuration.
func DefaultPreferCopyHeredocConfig() PreferCopyHeredocConfig {
	checkSingle := true
	checkConsecutive := true
	return PreferCopyHeredocConfig{
		CheckSingleRun:       &checkSingle,
		CheckConsecutiveRuns: &checkConsecutive,
	}
}

// PreferCopyHeredocRule implements the prefer-copy-heredoc linting rule.
// It detects RUN instructions used for creating files and suggests using
// COPY <<EOF syntax instead.
type PreferCopyHeredocRule struct{}

// NewPreferCopyHeredocRule creates a new prefer-copy-heredoc rule instance.
func NewPreferCopyHeredocRule() *PreferCopyHeredocRule {
	return &PreferCopyHeredocRule{}
}

// Metadata returns the rule metadata.
func (r *PreferCopyHeredocRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferCopyHeredocRuleCode,
		Name:            "Prefer COPY heredoc for file creation",
		Description:     "Use COPY <<EOF syntax instead of RUN echo/cat for creating files",
		DocURL:          "https://github.com/tinovyatkin/tally/blob/main/docs/rules/prefer-copy-heredoc.md",
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  true,
		FixPriority:     99, // Run before prefer-run-heredoc (100)
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferCopyHeredocRule) Schema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"check-single-run": map[string]any{
				"type":        "boolean",
				"default":     true,
				"description": "Check for single RUN instructions with file creation",
			},
			"check-consecutive-runs": map[string]any{
				"type":        "boolean",
				"default":     true,
				"description": "Check for consecutive RUN instructions creating/appending to same file",
			},
		},
		"additionalProperties": false,
	}
}

// Check runs the prefer-copy-heredoc rule.
func (r *PreferCopyHeredocRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)

	checkSingle := cfg.CheckSingleRun == nil || *cfg.CheckSingleRun
	checkConsecutive := cfg.CheckConsecutiveRuns == nil || *cfg.CheckConsecutiveRuns

	var violations []rules.Violation
	meta := r.Metadata()
	sm := input.SourceMap()

	// Get semantic model for shell variant and variable info
	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Type assertion OK

	for stageIdx, stage := range input.Stages {
		// Get shell variant and variable scope for this stage
		shellVariant := shell.VariantBash
		var varScope *semantic.VariableScope
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				if shellVariant.IsNonPOSIX() {
					continue
				}
				varScope = info.Variables
			}
		}

		// Create variable checker
		knownVars := makeKnownVarsChecker(varScope)

		if checkSingle {
			violations = append(violations,
				r.checkSingleRuns(stage, shellVariant, knownVars, input.File, sm, meta)...)
		}

		if checkConsecutive {
			violations = append(violations,
				r.checkConsecutiveRuns(stage, shellVariant, knownVars, input.File, sm, meta)...)
		}
	}

	return violations
}

// DefaultConfig returns the default configuration for this rule.
func (r *PreferCopyHeredocRule) DefaultConfig() any {
	return DefaultPreferCopyHeredocConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferCopyHeredocRule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

// fileCreationRun represents a RUN instruction that creates a file.
type fileCreationRun struct {
	run  *instructions.RunCommand
	info *shell.FileCreationInfo
}

// checkSingleRuns checks individual RUN instructions for file creation patterns.
func (r *PreferCopyHeredocRule) checkSingleRuns( //nolint:funlen // Slightly over limit due to two-pass algorithm
	stage instructions.Stage,
	shellVariant shell.Variant,
	knownVars func(string) bool,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation //nolint:prealloc // Size unknown until iteration completes

	// First pass: identify which RUNs are part of consecutive sequences
	// These will be handled by checkConsecutiveRuns, so skip them here
	inSequence := make(map[*instructions.RunCommand]bool)
	var prevInfo *shell.FileCreationInfo
	var prevRun *instructions.RunCommand

	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell || len(run.Files) > 0 {
			prevInfo = nil
			prevRun = nil
			continue
		}

		script := getRunScriptFromCmd(run)
		if script == "" {
			prevInfo = nil
			prevRun = nil
			continue
		}

		info := shell.DetectFileCreation(script, shellVariant, knownVars)
		if info == nil || info.HasUnsafeVariables {
			prevInfo = nil
			prevRun = nil
			continue
		}

		// Check if this continues a sequence (same target file)
		if prevInfo != nil && prevRun != nil && info.TargetPath == prevInfo.TargetPath {
			inSequence[prevRun] = true
			inSequence[run] = true
		}

		prevInfo = info
		prevRun = run
	}

	// Second pass: report violations for standalone file creations
	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok {
			continue
		}

		// Skip if part of a consecutive sequence
		if inSequence[run] {
			continue
		}

		// Only check shell form
		if !run.PrependShell {
			continue
		}

		// Skip heredoc RUNs (already using heredoc)
		if len(run.Files) > 0 {
			continue
		}

		script := getRunScriptFromCmd(run)
		if script == "" {
			continue
		}

		// Detect file creation pattern
		info := shell.DetectFileCreation(script, shellVariant, knownVars)
		if info == nil {
			continue
		}

		// Skip if uses unsafe variables
		if info.HasUnsafeVariables {
			continue
		}

		// Skip appends in single-run check (handled by consecutive check)
		if info.IsAppend {
			continue
		}

		// Create violation
		loc := rules.NewLocationFromRanges(file, run.Location())

		v := rules.NewViolation(
			loc,
			meta.Code,
			"use COPY <<EOF instead of RUN for file creation",
			meta.DefaultSeverity,
		).WithDocURL(meta.DocURL).WithDetail(
			fmt.Sprintf("Creating %s with RUN can be replaced with COPY heredoc for better performance", info.TargetPath),
		)

		// Generate fix
		if fix := r.generateFix(run, info, file, sm, meta); fix != nil {
			v = v.WithSuggestedFix(fix)
		}

		violations = append(violations, v)
	}

	return violations
}

// checkConsecutiveRuns checks for sequences of RUN instructions that write to the same file.
func (r *PreferCopyHeredocRule) checkConsecutiveRuns(
	stage instructions.Stage,
	shellVariant shell.Variant,
	knownVars func(string) bool,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	var sequence []fileCreationRun
	var targetPath string

	flushSequence := func() {
		if v := r.createSequenceViolation(sequence, targetPath, file, sm, meta); v != nil {
			violations = append(violations, *v)
		}
		sequence = nil
		targetPath = ""
	}

	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok {
			flushSequence()
			continue
		}

		// Only check shell form
		if !run.PrependShell {
			flushSequence()
			continue
		}

		// Skip heredoc RUNs
		if len(run.Files) > 0 {
			flushSequence()
			continue
		}

		script := getRunScriptFromCmd(run)
		if script == "" {
			flushSequence()
			continue
		}

		// Detect file creation pattern
		info := shell.DetectFileCreation(script, shellVariant, knownVars)
		if info == nil {
			flushSequence()
			continue
		}

		// Skip if uses unsafe variables
		if info.HasUnsafeVariables {
			flushSequence()
			continue
		}

		// Check if this continues the sequence
		switch {
		case len(sequence) == 0:
			// Start new sequence
			sequence = append(sequence, fileCreationRun{run: run, info: info})
			targetPath = info.TargetPath
		case info.TargetPath == targetPath:
			// Continue sequence (must be append or overwrites)
			sequence = append(sequence, fileCreationRun{run: run, info: info})
		default:
			// Different file - flush and start new sequence
			flushSequence()
			sequence = append(sequence, fileCreationRun{run: run, info: info})
			targetPath = info.TargetPath
		}
	}

	flushSequence()
	return violations
}

// createSequenceViolation creates a violation for a sequence of file creation RUNs.
func (r *PreferCopyHeredocRule) createSequenceViolation(
	sequence []fileCreationRun,
	targetPath string,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.Violation {
	// Need at least 2 RUNs to be a sequence
	if len(sequence) < 2 {
		return nil
	}

	firstRun := sequence[0].run
	loc := rules.NewLocationFromRanges(file, firstRun.Location())

	v := rules.NewViolation(
		loc,
		meta.Code,
		"consecutive RUN file creations can use a single COPY heredoc",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		fmt.Sprintf("%d consecutive RUN instructions write to %s; combine into single COPY <<EOF", len(sequence), targetPath),
	)

	// Generate fix for the sequence
	if fix := r.generateSequenceFix(sequence, targetPath, file, sm, meta); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

// generateFix generates a fix for a single RUN file creation.
// Handles mixed commands by splitting into separate instructions.
func (r *PreferCopyHeredocRule) generateFix(
	run *instructions.RunCommand,
	info *shell.FileCreationInfo,
	file string,
	_ *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	// Build the replacement text
	var parts []string

	// Add preceding commands as RUN if any
	if info.PrecedingCommands != "" {
		parts = append(parts, "RUN "+info.PrecedingCommands)
	}

	// Add COPY heredoc for the file creation
	copyCmd := buildCopyHeredoc(info.TargetPath, info.Content, info.ChmodMode)
	parts = append(parts, copyCmd)

	// Add remaining commands as RUN if any
	if info.RemainingCommands != "" {
		parts = append(parts, "RUN "+info.RemainingCommands)
	}

	newText := strings.Join(parts, "\n")

	// Calculate edit range - handle invalid end positions
	lastRange := runLoc[len(runLoc)-1]
	endLine := lastRange.End.Line
	endCol := lastRange.End.Character

	// When end position equals start position (invalid), calculate from command length
	if endLine == runLoc[0].Start.Line && endCol == runLoc[0].Start.Character {
		cmdStr := getRunScriptFromCmd(run)
		fullInstr := "RUN " + cmdStr

		// For multi-line instructions, count lines and find last line length
		lines := strings.Split(fullInstr, "\n")
		if len(lines) > 1 {
			// Multi-line: endLine is start + number of additional lines
			// endCol is the length of the last line
			endLine = runLoc[0].Start.Line + len(lines) - 1
			endCol = len(lines[len(lines)-1])
		} else {
			// Single line: endCol is start + instruction length
			endCol = runLoc[0].Start.Character + len(fullInstr)
		}
	}

	description := "Replace RUN with COPY <<EOF to " + info.TargetPath
	if info.PrecedingCommands != "" || info.RemainingCommands != "" {
		description = "Extract file creation to COPY <<EOF"
	}

	return &rules.SuggestedFix{
		Description: description,
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
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

// generateSequenceFix generates a fix for a sequence of RUN file creations.
func (r *PreferCopyHeredocRule) generateSequenceFix(
	sequence []fileCreationRun,
	targetPath string,
	file string,
	_ *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	if len(sequence) == 0 {
		return nil
	}

	// Merge content from all RUNs
	var content strings.Builder
	var chmodMode string

	for i, fcr := range sequence {
		if i > 0 && !fcr.info.IsAppend {
			// Overwrite - clear previous content
			content.Reset()
		}
		content.WriteString(fcr.info.Content)

		// Use last chmod mode
		if fcr.info.ChmodMode != "" {
			chmodMode = fcr.info.ChmodMode
		}
	}

	// Build COPY heredoc
	copyCmd := buildCopyHeredoc(targetPath, content.String(), chmodMode)

	// Calculate edit range - from first RUN to last RUN
	firstLoc := sequence[0].run.Location()
	lastLoc := sequence[len(sequence)-1].run.Location()

	if len(firstLoc) == 0 || len(lastLoc) == 0 {
		return nil
	}

	lastRange := lastLoc[len(lastLoc)-1]
	endLine := lastRange.End.Line
	endCol := lastRange.End.Character

	// When end position equals start position (invalid), calculate from command length
	lastRun := sequence[len(sequence)-1].run
	lastRunLoc := lastRun.Location()
	if len(lastRunLoc) > 0 &&
		endLine == lastRunLoc[0].Start.Line && endCol == lastRunLoc[0].Start.Character {
		cmdStr := getRunScriptFromCmd(lastRun)
		fullInstr := "RUN " + cmdStr
		endCol = lastRunLoc[0].Start.Character + len(fullInstr)
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Combine %d RUNs into single COPY <<EOF to %s", len(sequence), targetPath),
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(
				file,
				firstLoc[0].Start.Line,
				firstLoc[0].Start.Character,
				endLine,
				endCol,
			),
			NewText: copyCmd,
		}},
	}
}

// buildCopyHeredoc builds a COPY heredoc instruction.
func buildCopyHeredoc(targetPath, content, chmodMode string) string {
	var sb strings.Builder
	sb.WriteString("COPY ")

	if chmodMode != "" {
		sb.WriteString("--chmod=")
		sb.WriteString(shell.NormalizeOctalMode(chmodMode))
		sb.WriteString(" ")
	}

	// Choose delimiter that doesn't conflict with content
	delimiter := chooseDelimiter(content)

	sb.WriteString("<<")
	sb.WriteString(delimiter)
	sb.WriteString(" ")
	sb.WriteString(targetPath)
	sb.WriteString("\n")

	// Write content (ensure no trailing newline duplication)
	contentStr := strings.TrimSuffix(content, "\n")
	sb.WriteString(contentStr)
	sb.WriteString("\n")

	sb.WriteString(delimiter)

	return sb.String()
}

// chooseDelimiter selects a heredoc delimiter that doesn't appear in content.
func chooseDelimiter(content string) string {
	delimiters := []string{"EOF", "CONTENT", "FILE", "DATA", "END"}
	for _, d := range delimiters {
		if !strings.Contains(content, d) {
			return d
		}
	}
	// Fallback with number suffix
	for i := 1; i < 100; i++ {
		d := fmt.Sprintf("EOF%d", i)
		if !strings.Contains(content, d) {
			return d
		}
	}
	return "EOF"
}

// resolveConfig extracts the PreferCopyHeredocConfig from input.
func (r *PreferCopyHeredocRule) resolveConfig(config any) PreferCopyHeredocConfig {
	switch v := config.(type) {
	case PreferCopyHeredocConfig:
		return v
	case *PreferCopyHeredocConfig:
		if v != nil {
			return *v
		}
	case map[string]any:
		return configutil.Resolve(v, DefaultPreferCopyHeredocConfig())
	}
	return DefaultPreferCopyHeredocConfig()
}

// makeKnownVarsChecker creates a function that checks if a variable is a known ARG/ENV.
func makeKnownVarsChecker(scope *semantic.VariableScope) func(string) bool {
	if scope == nil {
		return nil
	}
	return func(name string) bool {
		return scope.HasArg(name) || scope.GetEnv(name) != nil
	}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferCopyHeredocRule())
}
