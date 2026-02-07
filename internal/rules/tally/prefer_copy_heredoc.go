// Package tally implements tally-specific linting rules for Dockerfiles.
package tally

import (
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
	"github.com/tinovyatkin/tally/internal/runmount"
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
				r.checkSingleRuns(stage, shellVariant, knownVars, input.File, sm, meta, checkConsecutive)...)
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

// identifySequenceRuns identifies which RUN instructions are part of consecutive sequences.
// These will be handled by checkConsecutiveRuns, so they should be skipped in checkSingleRuns.
// A sequence is: multiple file creations to same file, or file creation + chmod.
func identifySequenceRuns(
	stage instructions.Stage,
	shellVariant shell.Variant,
	knownVars func(string) bool,
) map[*instructions.RunCommand]bool {
	inSequence := make(map[*instructions.RunCommand]bool)
	var prevInfo *shell.FileCreationInfo
	var prevRun *instructions.RunCommand

	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			prevInfo, prevRun = nil, nil
			continue
		}

		script := getRunCmdLine(run)
		if script == "" {
			prevInfo, prevRun = nil, nil
			continue
		}

		// Check for file creation
		info := shell.DetectFileCreation(script, shellVariant, knownVars)
		if info != nil && !info.HasUnsafeVariables {
			// Skip if RUN has mounts that conflict with COPY conversion
			if shouldSkipForMounts(run, info.TargetPath) {
				prevInfo, prevRun = nil, nil
				continue
			}

			// Mixed commands can't be part of a sequence (handled by checkSingleRuns)
			if info.PrecedingCommands != "" || info.RemainingCommands != "" {
				prevInfo, prevRun = nil, nil
				continue
			}

			// Don't start sequence with append-only (unknown base content)
			if info.IsAppend && (prevInfo == nil || info.TargetPath != prevInfo.TargetPath) {
				prevInfo, prevRun = nil, nil
				continue
			}

			if prevInfo != nil && prevRun != nil && info.TargetPath == prevInfo.TargetPath {
				inSequence[prevRun] = true
				inSequence[run] = true
			}
			prevInfo, prevRun = info, run
			continue
		}

		// Check for standalone chmod that continues the sequence
		if prevInfo != nil && prevRun != nil {
			chmodInfo := shell.DetectStandaloneChmod(script, shellVariant)
			if chmodInfo != nil && chmodInfo.Target == prevInfo.TargetPath {
				inSequence[prevRun] = true
				inSequence[run] = true
				continue
			}
		}

		prevInfo, prevRun = nil, nil
	}
	return inSequence
}

// checkSingleRuns checks individual RUN instructions for file creation patterns.
// skipSequences controls whether to skip RUNs that are part of consecutive sequences
// (should be true when checkConsecutive is enabled, false otherwise).
func (r *PreferCopyHeredocRule) checkSingleRuns(
	stage instructions.Stage,
	shellVariant shell.Variant,
	knownVars func(string) bool,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
	skipSequences bool,
) []rules.Violation {
	var violations []rules.Violation //nolint:prealloc // Size unknown until iteration completes
	var inSequence map[*instructions.RunCommand]bool
	if skipSequences {
		inSequence = identifySequenceRuns(stage, shellVariant, knownVars)
	}

	// Report violations for standalone file creations
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

		script := getRunCmdLine(run)
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

		// Skip if RUN has mounts that conflict with COPY conversion
		if shouldSkipForMounts(run, info.TargetPath) {
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
	var sequenceChmodMode uint16                  // chmod mode from trailing RUN chmod
	var sequenceChmodRun *instructions.RunCommand // the RUN chmod instruction

	flushSequence := func() {
		if v := r.createSequenceViolation(sequence, targetPath, sequenceChmodMode, sequenceChmodRun, file, sm, meta); v != nil {
			violations = append(violations, *v)
		}
		sequence = nil
		targetPath = ""
		sequenceChmodMode = 0
		sequenceChmodRun = nil
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

		script := getRunCmdLine(run)
		if script == "" {
			flushSequence()
			continue
		}

		// Detect file creation pattern
		info := shell.DetectFileCreation(script, shellVariant, knownVars)
		if info != nil && !info.HasUnsafeVariables {
			// Skip if RUN has mounts that conflict with COPY conversion
			if shouldSkipForMounts(run, info.TargetPath) {
				flushSequence()
				continue
			}

			// Mixed commands can't be safely combined into a single COPY heredoc
			// (would drop PrecedingCommands or RemainingCommands)
			if info.PrecedingCommands != "" || info.RemainingCommands != "" {
				flushSequence()
				continue
			}

			// Do not start a sequence with append-only writes (unknown base content)
			if len(sequence) == 0 && info.IsAppend {
				flushSequence()
				continue
			}

			// Update sequence with this file creation
			sequence, targetPath = updateFileCreationSequence(
				sequence, targetPath, run, info, flushSequence,
			)
			if sequenceChmodMode != 0 {
				// Chmod is no longer trailing once another write appears.
				sequenceChmodRun = nil
				// Inline chmod overrides earlier standalone chmod.
				if info.ChmodMode != 0 {
					sequenceChmodMode = 0
				}
			}
			continue
		}

		// Check for standalone chmod that can extend the sequence
		if len(sequence) > 0 && sequenceChmodMode == 0 {
			chmodInfo := shell.DetectStandaloneChmod(script, shellVariant)
			if chmodInfo != nil && chmodInfo.Target == targetPath {
				// This chmod targets our sequence's file - absorb it
				sequenceChmodMode = chmodInfo.Mode
				sequenceChmodRun = run
				continue
			}
		}

		// Neither file creation nor matching chmod - flush
		flushSequence()
	}

	flushSequence()
	return violations
}

// updateFileCreationSequence updates the sequence with a new file creation.
// Returns the updated sequence and target path.
func updateFileCreationSequence(
	sequence []fileCreationRun,
	targetPath string,
	run *instructions.RunCommand,
	info *shell.FileCreationInfo,
	flushSequence func(),
) ([]fileCreationRun, string) {
	switch {
	case len(sequence) == 0:
		// Start new sequence
		return []fileCreationRun{{run: run, info: info}}, info.TargetPath
	case info.TargetPath == targetPath:
		// Continue sequence (must be append or overwrites)
		return append(sequence, fileCreationRun{run: run, info: info}), targetPath
	default:
		// Different file - flush and start new sequence
		flushSequence()
		return []fileCreationRun{{run: run, info: info}}, info.TargetPath
	}
}

// createSequenceViolation creates a violation for a sequence of file creation RUNs.
func (r *PreferCopyHeredocRule) createSequenceViolation(
	sequence []fileCreationRun,
	targetPath string,
	chmodMode uint16,
	chmodRun *instructions.RunCommand,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.Violation {
	// Need at least 2 RUNs to be a sequence, or 1 RUN + chmod
	if len(sequence) < 2 && chmodRun == nil {
		return nil
	}
	if len(sequence) == 0 {
		return nil
	}

	firstRun := sequence[0].run
	loc := rules.NewLocationFromRanges(file, firstRun.Location())

	runCount := len(sequence)
	if chmodRun != nil {
		runCount++
	}

	v := rules.NewViolation(
		loc,
		meta.Code,
		"consecutive RUN file creations can use a single COPY heredoc",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		fmt.Sprintf("%d consecutive RUN instructions write to %s; combine into single COPY <<EOF", runCount, targetPath),
	)

	// Generate fix for the sequence
	if fix := r.generateSequenceFix(sequence, targetPath, chmodMode, chmodRun, file, sm, meta); fix != nil {
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
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	// Build the replacement text
	var parts []string

	// Get mount flags to preserve on remaining RUN commands
	mountFlags := runmount.FormatMounts(runmount.GetMounts(run))
	runPrefix := "RUN "
	if mountFlags != "" {
		runPrefix = "RUN " + mountFlags + " "
	}

	// Add preceding commands as RUN if any (preserve mounts)
	if info.PrecedingCommands != "" {
		parts = append(parts, runPrefix+info.PrecedingCommands)
	}

	// Add COPY heredoc for the file creation
	copyCmd := buildCopyHeredoc(info.TargetPath, info.Content, info.ChmodMode)
	parts = append(parts, copyCmd)

	// Add remaining commands as RUN if any (preserve mounts)
	if info.RemainingCommands != "" {
		parts = append(parts, runPrefix+info.RemainingCommands)
	}

	newText := strings.Join(parts, "\n")

	endLine, endCol := resolveRunEndPosition(runLoc, sm, run)

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
	trailingChmodMode uint16,
	trailingChmodRun *instructions.RunCommand,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	if len(sequence) == 0 {
		return nil
	}

	// Merge content from all RUNs
	var content strings.Builder
	var chmodMode uint16

	for i, fcr := range sequence {
		if i > 0 && !fcr.info.IsAppend {
			// Overwrite - clear previous content
			content.Reset()
		}
		content.WriteString(fcr.info.Content)

		// Use last chmod mode from file creation commands
		if fcr.info.ChmodMode != 0 {
			chmodMode = fcr.info.ChmodMode
		}
	}

	// Trailing RUN chmod overrides any inline chmod
	if trailingChmodMode != 0 {
		chmodMode = trailingChmodMode
	}

	// Build COPY heredoc
	copyCmd := buildCopyHeredoc(targetPath, content.String(), chmodMode)

	// Calculate edit range - from first RUN to last RUN (or trailing chmod)
	firstLoc := sequence[0].run.Location()
	if len(firstLoc) == 0 {
		return nil
	}

	// Determine the last RUN instruction (could be trailing chmod)
	var lastRun *instructions.RunCommand
	if trailingChmodRun != nil {
		lastRun = trailingChmodRun
	} else {
		lastRun = sequence[len(sequence)-1].run
	}

	lastLoc := lastRun.Location()
	if len(lastLoc) == 0 {
		return nil
	}

	endLine, endCol := resolveRunEndPosition(lastLoc, sm, lastRun)

	runCount := len(sequence)
	if trailingChmodRun != nil {
		runCount++
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Combine %d RUNs into single COPY <<EOF to %s", runCount, targetPath),
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
func buildCopyHeredoc(targetPath, content string, chmodMode uint16) string {
	var sb strings.Builder
	sb.WriteString("COPY ")

	if chmodMode != 0 {
		sb.WriteString("--chmod=")
		sb.WriteString(shell.FormatOctalMode(chmodMode))
		sb.WriteString(" ")
	}

	// Choose delimiter that doesn't conflict with content
	delimiter := chooseDelimiter(content)

	sb.WriteString("<<")
	sb.WriteString(delimiter)
	sb.WriteString(" ")
	sb.WriteString(targetPath)
	sb.WriteString("\n")

	if content == "" {
		// Empty file: delimiter immediately after header newline (0-byte file)
		sb.WriteString(delimiter)
		return sb.String()
	}
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
	return configutil.Coerce(config, DefaultPreferCopyHeredocConfig())
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

// shouldSkipForMounts checks if a RUN instruction should be skipped due to mounts.
// COPY doesn't support --mount, so we need to be careful about when to suggest conversion.
//
// Safety by mount type:
//   - bind: SKIP - content might depend on bound files, can't verify at lint time
//   - cache: SAFE if file target is outside cache path (cache is for build artifacts)
//   - tmpfs: SAFE if file target is outside tmpfs path (tmpfs is temp space)
//   - secret: SAFE if file target is outside secret path (our HasUnsafeVariables catches $(cat /secret/...))
//   - ssh: SAFE - no target path that affects file content
func shouldSkipForMounts(run *instructions.RunCommand, fileTarget string) bool {
	mounts := runmount.GetMounts(run)
	if len(mounts) == 0 {
		return false
	}

	for _, m := range mounts {
		switch m.Type {
		case instructions.MountTypeBind:
			// Bind mounts are risky - content might depend on bound files
			// and we can't verify the content source at lint time
			return true

		case instructions.MountTypeCache, instructions.MountTypeTmpfs:
			// Safe unless writing to mount target (file won't persist)
			if m.Target != "" && isPathUnder(fileTarget, m.Target) {
				return true
			}

		case instructions.MountTypeSecret:
			// Safe if our content detection already validated it
			// (HasUnsafeVariables catches $(cat /run/secrets/...))
			// But skip if writing to secret path (unusual but possible)
			if m.Target != "" && isPathUnder(fileTarget, m.Target) {
				return true
			}

		case instructions.MountTypeSSH:
			// Always safe - SSH just forwards agent socket, no content dependency
			continue
		}
	}

	return false
}

// isPathUnder checks if path is under or equal to base directory.
func isPathUnder(path, base string) bool {
	path = pathpkg.Clean(path)
	base = pathpkg.Clean(base)
	// Normalize: ensure base ends with / for proper prefix matching
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	// Check if path equals base (without trailing slash) or is under it
	return path == strings.TrimSuffix(base, "/") || strings.HasPrefix(path, base)
}

// getRunCmdLine extracts the command line from a RUN instruction for shell parsing.
// Unlike getRunScriptFromCmd (which returns heredoc content), this reconstructs the
// full shell script including heredoc content, which is needed to detect redirects
// in commands like "cat <<EOF > /file".
func getRunCmdLine(run *instructions.RunCommand) string {
	if len(run.CmdLine) == 0 {
		return ""
	}

	cmdLine := strings.Join(run.CmdLine, " ")

	// If there are heredocs, reconstruct the full script
	if len(run.Files) > 0 {
		var sb strings.Builder
		sb.WriteString(cmdLine)
		for _, f := range run.Files {
			sb.WriteString("\n")
			sb.WriteString(f.Data)
			sb.WriteString(f.Name)
		}
		return sb.String()
	}

	return cmdLine
}

// resolveRunEndPosition computes the end position for a RUN instruction using
// resolveEndPosition, with a fallback for point locations (End == Start) where
// the end is computed from the command text.
func resolveRunEndPosition(loc []parser.Range, sm *sourcemap.SourceMap, run *instructions.RunCommand) (int, int) {
	if len(loc) == 0 {
		return 0, 0
	}

	endLine, endCol := resolveEndPosition(loc, sm)

	// Fallback: when end position equals start (point location), compute from command text
	if endLine == loc[0].Start.Line && endCol == loc[0].Start.Character {
		cmdStr := getRunScriptFromCmd(run)
		mountFlags := runmount.FormatMounts(runmount.GetMounts(run))
		fullInstr := "RUN "
		if mountFlags != "" {
			fullInstr += mountFlags + " "
		}
		fullInstr += cmdStr

		lines := strings.Split(fullInstr, "\n")
		if len(lines) > 1 {
			endLine = loc[0].Start.Line + len(lines) - 1
			endCol = len(lines[len(lines)-1])
		} else {
			endCol = loc[0].Start.Character + len(fullInstr)
		}
	}

	return endLine, endCol
}

// resolveEndPosition computes the correct end position for a RUN instruction's edit range.
// It handles a BuildKit parser edge case where heredoc instructions report
// End={delimiterLine, 0}, which in half-open semantics covers up to—but not
// including—the delimiter text. We extend to cover the full delimiter line.
func resolveEndPosition(loc []parser.Range, sm *sourcemap.SourceMap) (int, int) {
	if len(loc) == 0 {
		return 0, 0
	}

	lastRange := loc[len(loc)-1]
	endLine := lastRange.End.Line
	endCol := lastRange.End.Character

	startLine := loc[0].Start.Line

	// Heredoc delimiter case: BuildKit reports End={delimiterLine, 0} for
	// heredoc instructions. In half-open interval semantics, column 0 means
	// the range ends at the START of that line, not covering the delimiter text.
	// Extend to cover the full line using the SourceMap's line content.
	if endCol == 0 && endLine > startLine && sm != nil {
		// SourceMap uses 0-based lines; BuildKit uses 1-based
		lineText := sm.Line(endLine - 1)
		endCol = len(lineText)
	}

	return endLine, endCol
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferCopyHeredocRule())
}
