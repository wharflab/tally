// Package tally implements tally-specific linting rules for Dockerfiles.
package tally

import (
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
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
type PreferCopyHeredocRule struct {
	schema map[string]any
}

// NewPreferCopyHeredocRule creates a new prefer-copy-heredoc rule instance.
func NewPreferCopyHeredocRule() *PreferCopyHeredocRule {
	schema, err := configutil.RuleSchema(PreferCopyHeredocRuleCode)
	if err != nil {
		panic(err)
	}
	return &PreferCopyHeredocRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *PreferCopyHeredocRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferCopyHeredocRuleCode,
		Name:            "Prefer COPY heredoc for file creation",
		Description:     "Use COPY <<EOF syntax instead of RUN echo/cat/printf for creating files",
		DocURL:          rules.TallyDocURL(PreferCopyHeredocRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		IsExperimental:  false,
		FixPriority:     99, // Run before prefer-run-heredoc (100)
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferCopyHeredocRule) Schema() map[string]any {
	return r.schema
}

// Check runs the prefer-copy-heredoc rule.
func (r *PreferCopyHeredocRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)

	checkSingle := cfg.CheckSingleRun == nil || *cfg.CheckSingleRun
	checkConsecutive := cfg.CheckConsecutiveRuns == nil || *cfg.CheckConsecutiveRuns

	var violations []rules.Violation
	meta := r.Metadata()
	sm := input.SourceMap()
	fileFacts := input.Facts

	// Get semantic model for shell variant and variable info
	var sem = input.Semantic

	for stageIdx, stage := range input.Stages {
		// Get fallback shell variant and variable scope for this stage.
		shellVariant := shell.VariantBash
		var varScope *semantic.VariableScope
		var stageInfo *semantic.StageInfo
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				stageInfo = info
				shellVariant = info.ShellSetting.Variant
				varScope = info.Variables
			}
		}

		// Create variable checker
		knownVars := makeKnownVarsChecker(varScope)

		ctx := copyHeredocCheckContext{
			stageIdx:        stageIdx,
			stage:           stage,
			fileFacts:       fileFacts,
			sem:             sem,
			stageInfo:       stageInfo,
			fallbackVariant: shellVariant,
			knownVars:       knownVars,
			file:            input.File,
			sm:              sm,
			meta:            meta,
		}

		if checkSingle {
			violations = append(violations,
				r.checkSingleRuns(ctx, checkConsecutive)...)
		}

		if checkConsecutive {
			violations = append(violations,
				r.checkConsecutiveRuns(ctx)...)
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
	return configutil.ValidateRuleOptions(PreferCopyHeredocRuleCode, config)
}

// fileCreationRun represents a RUN instruction that creates a file.
type fileCreationRun struct {
	run  *instructions.RunCommand
	info *shell.FileCreationInfo
}

// copyHeredocCheckContext holds shared parameters for prefer-copy-heredoc check methods.
type copyHeredocCheckContext struct {
	stageIdx        int
	stage           instructions.Stage
	fileFacts       *facts.FileFacts
	sem             *semantic.Model
	stageInfo       *semantic.StageInfo
	fallbackVariant shell.Variant
	knownVars       func(string) bool
	file            string
	sm              *sourcemap.SourceMap
	meta            rules.RuleMetadata
}

// copyHeredocSequence tracks the state of a consecutive file creation sequence.
type copyHeredocSequence struct {
	runs     []fileCreationRun
	target   string
	rawChmod string
	chmodRun *instructions.RunCommand
}

func (s *copyHeredocSequence) reset() {
	s.runs = nil
	s.target = ""
	s.rawChmod = ""
	s.chmodRun = nil
}

// identifySequenceRuns identifies which RUN instructions are part of consecutive sequences.
// These will be handled by checkConsecutiveRuns, so they should be skipped in checkSingleRuns.
// A sequence is: multiple file creations to same file, or file creation + chmod.
func identifySequenceRuns(ctx copyHeredocCheckContext) map[*instructions.RunCommand]bool {
	inSequence := make(map[*instructions.RunCommand]bool)
	var prevInfo *shell.FileCreationInfo
	var prevRun *instructions.RunCommand
	userState := newPreferCopyHeredocUserState(ctx.stageIdx, ctx.sem, ctx.fileFacts)

	for _, cmd := range ctx.stage.Commands {
		switch c := cmd.(type) {
		case *instructions.UserCommand:
			userState.currentUser = c.User
			prevInfo, prevRun = nil, nil
		case *instructions.RunCommand:
			shellCtx := preferCopyHeredocShellContextForRun(c, ctx.stageInfo, ctx.fallbackVariant)
			info := detectRunFileCreation(c, shellCtx.variant, shellCtx.fileCreationOptions, ctx.knownVars, userState)
			userState.learnHomesFromRun(c, shellCtx.variant)

			if !c.PrependShell {
				prevInfo, prevRun = nil, nil
				continue
			}

			if info != nil && !info.HasUnsafeVariables {
				// Skip if RUN has mounts that conflict with COPY conversion
				if shouldSkipForMounts(c, info.TargetPath) {
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
					inSequence[c] = true
				}
				prevInfo, prevRun = info, c
				continue
			}

			// Check for standalone chmod that continues the sequence
			if prevInfo != nil && prevRun != nil {
				script := getRunCmdLine(c)
				chmodInfo := shell.DetectStandaloneChmod(script, shellCtx.variant)
				if chmodInfo != nil && chmodInfo.Target == prevInfo.TargetPath {
					inSequence[prevRun] = true
					inSequence[c] = true
					continue
				}
			}

			prevInfo, prevRun = nil, nil
		default:
			prevInfo, prevRun = nil, nil
		}
	}
	return inSequence
}

// checkSingleRuns checks individual RUN instructions for file creation patterns.
// skipSequences controls whether to skip RUNs that are part of consecutive sequences
// (should be true when checkConsecutive is enabled, false otherwise).
func (r *PreferCopyHeredocRule) checkSingleRuns(
	ctx copyHeredocCheckContext,
	skipSequences bool,
) []rules.Violation {
	violations := make([]rules.Violation, 0, len(ctx.stage.Commands))
	var inSequence map[*instructions.RunCommand]bool
	if skipSequences {
		inSequence = identifySequenceRuns(ctx)
	}
	userState := newPreferCopyHeredocUserState(ctx.stageIdx, ctx.sem, ctx.fileFacts)

	// Report violations for standalone file creations
	for _, cmd := range ctx.stage.Commands {
		switch c := cmd.(type) {
		case *instructions.UserCommand:
			userState.currentUser = c.User
		case *instructions.RunCommand:
			shellCtx := preferCopyHeredocShellContextForRun(c, ctx.stageInfo, ctx.fallbackVariant)
			info := detectRunFileCreation(c, shellCtx.variant, shellCtx.fileCreationOptions, ctx.knownVars, userState)
			userState.learnHomesFromRun(c, shellCtx.variant)

			// Skip if part of a consecutive sequence
			if inSequence[c] {
				continue
			}

			// Only check shell form
			if !c.PrependShell || info == nil {
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
			if shouldSkipForMounts(c, info.TargetPath) {
				continue
			}

			// Create violation
			loc := rules.NewLocationFromRanges(ctx.file, c.Location())

			v := rules.NewViolation(
				loc,
				ctx.meta.Code,
				"use COPY <<EOF instead of RUN for file creation",
				ctx.meta.DefaultSeverity,
			).WithDocURL(ctx.meta.DocURL).WithDetail(
				fmt.Sprintf("Creating %s with RUN can be replaced with COPY heredoc for better performance", info.TargetPath),
			)

			// Generate fix
			if fix := r.generateFix(c, info, ctx.file, ctx.sm, ctx.meta, userState.currentUser); fix != nil {
				v = v.WithSuggestedFix(fix)
			}

			violations = append(violations, v)
		}
	}

	return violations
}

// checkConsecutiveRuns checks for sequences of RUN instructions that write to the same file.
func (r *PreferCopyHeredocRule) checkConsecutiveRuns(ctx copyHeredocCheckContext) []rules.Violation {
	var violations []rules.Violation
	var seq copyHeredocSequence
	userState := newPreferCopyHeredocUserState(ctx.stageIdx, ctx.sem, ctx.fileFacts)

	flushSequence := func() {
		if v := r.createSequenceViolation(
			seq.runs, seq.target, seq.rawChmod, seq.chmodRun, ctx, userState.currentUser,
		); v != nil {
			violations = append(violations, *v)
		}
		seq.reset()
	}

	for _, cmd := range ctx.stage.Commands {
		switch c := cmd.(type) {
		case *instructions.UserCommand:
			userState.currentUser = c.User
			flushSequence()
		case *instructions.RunCommand:
			if r.handleRunInSequence(c, ctx, userState, &seq, flushSequence) {
				continue
			}
			flushSequence()
		default:
			flushSequence()
		}
	}

	flushSequence()
	return violations
}

// handleRunInSequence processes a RunCommand for the consecutive sequence check.
// Returns true if the command was absorbed into the sequence, false if the caller should flush.
func (r *PreferCopyHeredocRule) handleRunInSequence(
	c *instructions.RunCommand,
	ctx copyHeredocCheckContext,
	userState *preferCopyHeredocUserState,
	seq *copyHeredocSequence,
	flushSequence func(),
) bool {
	shellCtx := preferCopyHeredocShellContextForRun(c, ctx.stageInfo, ctx.fallbackVariant)
	info := detectRunFileCreation(c, shellCtx.variant, shellCtx.fileCreationOptions, ctx.knownVars, userState)
	userState.learnHomesFromRun(c, shellCtx.variant)

	if !c.PrependShell {
		return false
	}

	script := getRunCmdLine(c)
	if script == "" {
		return false
	}

	// Detect file creation pattern
	if info != nil && !info.HasUnsafeVariables {
		if shouldSkipForMounts(c, info.TargetPath) {
			return false
		}
		if info.PrecedingCommands != "" || info.RemainingCommands != "" {
			return false
		}
		if len(seq.runs) == 0 && info.IsAppend {
			return false
		}

		seq.runs, seq.target = updateFileCreationSequence(
			seq.runs, seq.target, c, info, flushSequence,
		)
		if seq.rawChmod != "" {
			// Chmod is no longer trailing once another write appears.
			seq.chmodRun = nil
			// Inline chmod overrides earlier standalone chmod.
			if info.ChmodMode != 0 {
				seq.rawChmod = ""
			}
		}
		return true
	}

	// Check for standalone chmod that can extend the sequence
	if len(seq.runs) > 0 && seq.rawChmod == "" {
		chmodInfo := shell.DetectStandaloneChmod(script, shellCtx.variant)
		if chmodInfo != nil && chmodInfo.Target == seq.target {
			seq.rawChmod = chmodInfo.RawMode
			seq.chmodRun = c
			return true
		}
	}

	return false
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
	targetPath, rawChmodMode string,
	chmodRun *instructions.RunCommand,
	ctx copyHeredocCheckContext,
	effectiveUser string,
) *rules.Violation {
	// Need at least 2 RUNs to be a sequence, or 1 RUN + chmod
	if len(sequence) < 2 && chmodRun == nil {
		return nil
	}
	if len(sequence) == 0 {
		return nil
	}

	firstRun := sequence[0].run
	loc := rules.NewLocationFromRanges(ctx.file, firstRun.Location())

	runCount := len(sequence)
	if chmodRun != nil {
		runCount++
	}

	v := rules.NewViolation(
		loc,
		ctx.meta.Code,
		"consecutive RUN file creations can use a single COPY heredoc",
		ctx.meta.DefaultSeverity,
	).WithDocURL(ctx.meta.DocURL).WithDetail(
		fmt.Sprintf("%d consecutive RUN instructions write to %s; combine into single COPY <<EOF", runCount, targetPath),
	)

	// Generate fix for the sequence
	if fix := r.generateSequenceFix(
		sequence, targetPath, rawChmodMode, chmodRun, ctx, effectiveUser,
	); fix != nil {
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
	effectiveUser string,
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
	chownUser := chownUserForCopy(effectiveUser)
	copyCmd := buildCopyHeredoc(info.TargetPath, info.Content, info.RawChmodMode, chownUser)
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

	safety := rules.FixSuggestion
	if info.ResolvedHomePath {
		safety = rules.FixUnsafe
	}

	return &rules.SuggestedFix{
		Description: description,
		Safety:      safety,
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
	targetPath, trailingRawChmodMode string,
	trailingChmodRun *instructions.RunCommand,
	ctx copyHeredocCheckContext,
	effectiveUser string,
) *rules.SuggestedFix {
	if len(sequence) == 0 {
		return nil
	}

	// Merge content from all RUNs
	var content strings.Builder
	var rawChmodMode string

	for i, fcr := range sequence {
		if i > 0 && !fcr.info.IsAppend {
			// Overwrite - clear previous content
			content.Reset()
		}
		content.WriteString(fcr.info.Content)

		// Use last chmod mode from file creation commands
		if fcr.info.RawChmodMode != "" {
			rawChmodMode = fcr.info.RawChmodMode
		}
	}

	// Trailing RUN chmod overrides any inline chmod
	if trailingRawChmodMode != "" {
		rawChmodMode = trailingRawChmodMode
	}

	safety := rules.FixSuggestion
	for _, fcr := range sequence {
		if fcr.info != nil && fcr.info.ResolvedHomePath {
			safety = rules.FixUnsafe
			break
		}
	}

	// Build COPY heredoc
	chownUser := chownUserForCopy(effectiveUser)
	copyCmd := buildCopyHeredoc(targetPath, content.String(), rawChmodMode, chownUser)

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

	endLine, endCol := resolveRunEndPosition(lastLoc, ctx.sm, lastRun)

	runCount := len(sequence)
	if trailingChmodRun != nil {
		runCount++
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Combine %d RUNs into single COPY <<EOF to %s", runCount, targetPath),
		Safety:      safety,
		Priority:    ctx.meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(
				ctx.file,
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
// rawChmodMode is the original mode notation (e.g. "+x", "755"); used directly since
// COPY --chmod supports both octal and symbolic modes (Dockerfile 1.14+).
func buildCopyHeredoc(targetPath, content, rawChmodMode, chownUser string) string {
	var sb strings.Builder
	sb.WriteString("COPY ")

	if chownUser != "" {
		sb.WriteString("--chown=")
		sb.WriteString(chownUser)
		sb.WriteString(" ")
	}

	if rawChmodMode != "" {
		sb.WriteString("--chmod=")
		sb.WriteString(rawChmodMode)
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

// chownUserForCopy returns the user string for --chown when the active USER is
// non-root. Returns "" (no --chown needed) when the user is root, empty, or a
// variable/numeric reference that can't be safely embedded.
func chownUserForCopy(effectiveUser string) string {
	user := strings.TrimSpace(effectiveUser)
	if user == "" || facts.IsRootUser(user) {
		return ""
	}
	// Skip variable references (e.g., $APP_USER) — can't safely embed.
	if strings.ContainsAny(user, "${}") {
		return ""
	}
	return user
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

type preferCopyHeredocUserState struct {
	currentUser string
	knownHomes  map[string]string
}

func newPreferCopyHeredocUserState(
	stageIdx int,
	sem *semantic.Model,
	fileFacts *facts.FileFacts,
) *preferCopyHeredocUserState {
	state := &preferCopyHeredocUserState{
		knownHomes: make(map[string]string),
	}

	if sem != nil && fileFacts != nil {
		state.currentUser = inheritedUserForCopyChown(sem, fileFacts, stageIdx)
	}

	return state
}

func detectRunFileCreation(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	options shell.FileCreationOptions,
	knownVars func(string) bool,
	userState *preferCopyHeredocUserState,
) *shell.FileCreationInfo {
	if run == nil || !run.PrependShell {
		return nil
	}

	script := getRunCmdLine(run)
	if script == "" {
		return nil
	}

	if userState != nil {
		options.ResolveTargetPath = userState.resolveTargetPath
	}

	if options.ResolveTargetPath == nil && !options.InterpretPlainEchoEscapes {
		return shell.DetectFileCreation(script, shellVariant, knownVars)
	}

	return shell.DetectFileCreationWithOptions(script, shellVariant, knownVars, options)
}

type preferCopyHeredocShellContext struct {
	variant             shell.Variant
	fileCreationOptions shell.FileCreationOptions
}

func preferCopyHeredocShellContextForRun(
	run *instructions.RunCommand,
	stageInfo *semantic.StageInfo,
	fallbackVariant shell.Variant,
) preferCopyHeredocShellContext {
	var line int
	if run != nil {
		if locs := run.Location(); len(locs) > 0 {
			line = locs[0].Start.Line
		}
	}

	variant := fallbackVariant
	var shellName string
	if stageInfo != nil {
		if override, ok := preferCopyHeredocHeredocShellOverride(stageInfo, line); ok {
			variant = override.Variant
			shellName = override.Shell
		} else if line > 0 {
			variant = stageInfo.ShellVariantAtLine(line)
			shellName = stageInfo.ShellNameAtLine(line)
		} else {
			variant = stageInfo.ShellSetting.Variant
			if len(stageInfo.ShellSetting.Shell) > 0 {
				shellName = stageInfo.ShellSetting.Shell[0]
			}
		}
	}

	return preferCopyHeredocShellContext{
		variant: variant,
		fileCreationOptions: shell.FileCreationOptions{
			InterpretPlainEchoEscapes: preferCopyHeredocInterpretsPlainEchoEscapes(shellName, variant, isDashDefault(stageInfo)),
		},
	}
}

func preferCopyHeredocHeredocShellOverride(
	stageInfo *semantic.StageInfo,
	line int,
) (semantic.HeredocShellOverride, bool) {
	if stageInfo == nil || line <= 0 {
		return semantic.HeredocShellOverride{}, false
	}
	for _, override := range stageInfo.HeredocShellOverrides {
		if override.Line == line {
			return override, true
		}
	}
	return semantic.HeredocShellOverride{}, false
}

func isDashDefault(stageInfo *semantic.StageInfo) bool {
	return stageInfo != nil && stageInfo.DashDefault
}

func preferCopyHeredocInterpretsPlainEchoEscapes(shellName string, variant shell.Variant, dashDefault bool) bool {
	switch shell.NormalizeShellExecutableName(shellName) {
	case "dash":
		return true
	case "ash":
		return false
	case "sh":
		// When /bin/sh is the default and the variant is POSIX, the behavior
		// depends on the distro: dash (Debian/Ubuntu) interprets backslash
		// escapes in plain echo, ash (Alpine/BusyBox) does not.
		return variant == shell.VariantPOSIX && dashDefault
	default:
		return false
	}
}

func (s *preferCopyHeredocUserState) learnHomesFromRun(run *instructions.RunCommand, shellVariant shell.Variant) {
	if s == nil || run == nil || !run.PrependShell {
		return
	}

	script := getRunCmdLine(run)
	if script == "" {
		return
	}

	for _, cmd := range shell.FindCommands(script, shellVariant, "useradd", "adduser") {
		username, home := extractCreatedUserHome(&cmd)
		if username == "" || home == "" || !pathpkg.IsAbs(home) {
			continue
		}
		s.knownHomes[username] = pathpkg.Clean(home)
	}
}

func (s *preferCopyHeredocUserState) resolveTargetPath(rawTarget string) (string, bool, bool) {
	if rawTarget == "" {
		return "", false, false
	}
	if pathpkg.IsAbs(rawTarget) {
		return pathpkg.Clean(rawTarget), false, true
	}

	if rawTarget != "~" && !strings.HasPrefix(rawTarget, "~/") {
		return "", false, false
	}

	home, ok := s.currentHomeDir()
	if !ok {
		return "", false, false
	}
	if rawTarget == "~" {
		return home, true, true
	}

	return pathpkg.Join(home, strings.TrimPrefix(rawTarget, "~/")), true, true
}

func (s *preferCopyHeredocUserState) currentHomeDir() (string, bool) {
	user := effectiveUserNameForHome(s.currentUser)
	switch {
	case user == "", facts.IsRootUser(user):
		return "/root", true
	case !isResolvableNamedUser(user):
		return "", false
	case isNumericUser(user):
		return "", false
	default:
		if home := s.knownHomes[user]; home != "" {
			return home, true
		}
		return pathpkg.Join("/home", user), true
	}
}

func effectiveUserNameForHome(user string) string {
	user = strings.TrimSpace(user)
	if idx := strings.Index(user, ":"); idx >= 0 {
		user = user[:idx]
	}
	return strings.TrimSpace(user)
}

func isNumericUser(user string) bool {
	if user == "" {
		return false
	}
	for _, r := range user {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isResolvableNamedUser(user string) bool {
	if user == "" {
		return false
	}
	return !strings.ContainsAny(user, `$"'{}()[] \t`)
}

func extractCreatedUserHome(cmd *shell.CommandInfo) (string, string) {
	if cmd == nil {
		return "", ""
	}

	username := extractCreatedUsername(cmd)
	if username == "" {
		return "", ""
	}

	switch cmd.Name {
	case "useradd":
		if home := commandArgValueAny(cmd, "-d", "--home", "--home-dir"); home != "" {
			return username, home
		}
		if baseDir := commandArgValueAny(cmd, "-b", "--base-dir"); pathpkg.IsAbs(baseDir) {
			return username, pathpkg.Join(baseDir, username)
		}
	case "adduser":
		if home := commandArgValueAny(cmd, "-h", "--home", "--home-dir", "-d"); home != "" {
			return username, home
		}
	}

	return username, ""
}

func commandArgValueAny(cmd *shell.CommandInfo, flags ...string) string {
	for _, flag := range flags {
		if value := cmd.GetArgValue(flag); value != "" {
			return value
		}
	}
	return ""
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
