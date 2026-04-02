// Package fix provides fix application and resolution.
package fix

import (
	"bytes"
	"context"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/heredoc"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// heredocResolver implements FixResolver for prefer-run-heredoc fixes.
// Instead of trying to match original violations to new positions,
// it re-runs detection on the modified content and fixes what it finds.
type heredocResolver struct{}

// ID returns the resolver identifier.
func (r *heredocResolver) ID() string {
	return rules.HeredocResolverID
}

// Resolve re-runs heredoc detection on the current content and generates fixes.
// This approach is more robust than fingerprint matching because:
// - Content may have changed due to sync fixes (apt → apt-get, cd → WORKDIR)
// - Future rules may add mounts that break heredoc joining
// - No fragile matching logic needed
func (r *heredocResolver) Resolve(_ context.Context, resolveCtx ResolveContext, fix *rules.SuggestedFix) ([]rules.TextEdit, error) {
	data, ok := fix.ResolverData.(*rules.HeredocResolveData)
	if !ok {
		return nil, nil // Skip silently if data is wrong type
	}

	parseResult, err := dockerfile.Parse(bytes.NewReader(resolveCtx.Content), nil)
	if err != nil {
		return nil, nil //nolint:nilerr // Skip silently - don't fail fix process
	}

	// Validate stage index
	if data.StageIndex >= len(parseResult.Stages) {
		return nil, nil
	}
	stage := parseResult.Stages[data.StageIndex]
	sem := semantic.NewBuilder(parseResult, nil, resolveCtx.FilePath).Build()
	stageInfo := sem.StageInfo(data.StageIndex)

	// Create sourcemap for position calculations
	sm := sourcemap.New(resolveCtx.Content)

	// Re-run detection based on fix type
	switch data.Type {
	case rules.HeredocFixConsecutive:
		return r.detectAndFixConsecutive(stage, stageInfo, data, resolveCtx.FilePath, sm), nil
	case rules.HeredocFixChained:
		// Sync fixes can turn an originally chained single RUN into a better
		// consecutive-RUN opportunity (for example by inserting a SHELL that
		// lets adjacent RUNs share the same effective shell). Only upgrade when
		// the resulting sequence still includes the original violating RUN.
		if data.TargetStartLine > 0 {
			if edits := r.detectAndFixConsecutiveAtLine(
				stage,
				stageInfo,
				data,
				resolveCtx.FilePath,
				sm,
				data.TargetStartLine,
			); len(edits) > 0 {
				return edits, nil
			}
		}
		return r.detectAndFixChained(stage, stageInfo, data, resolveCtx.FilePath, sm), nil
	default:
		return nil, nil
	}
}

func (r *heredocResolver) detectAndFixConsecutiveAtLine(
	stage instructions.Stage,
	stageInfo *semantic.StageInfo,
	data *rules.HeredocResolveData,
	file string,
	sm *sourcemap.SourceMap,
	targetStartLine int,
) []rules.TextEdit {
	edits := r.detectAndFixConsecutive(stage, stageInfo, data, file, sm)
	if len(edits) == 0 {
		return nil
	}

	for _, edit := range edits {
		if edit.Location.Start.Line <= targetStartLine && targetStartLine <= edit.Location.End.Line {
			return []rules.TextEdit{edit}
		}
	}

	return nil
}

// runSequence holds a sequence of consecutive RUN instructions
// collected under the same effective shell variant.
type runSequence struct {
	runs         []*instructions.RunCommand
	commands     []string
	shellVariant shell.Variant // effective shell when this sequence was collected
}

// detectAndFixConsecutive finds all consecutive RUN sequences and returns edits for all of them.
// After sync fixes (e.g., prefer-copy-heredoc) may split a large consecutive group into
// multiple sub-groups, this ensures all qualifying sub-groups are fixed in one pass.
func (r *heredocResolver) detectAndFixConsecutive(
	stage instructions.Stage,
	stageInfo *semantic.StageInfo,
	data *rules.HeredocResolveData,
	file string,
	sm *sourcemap.SourceMap,
) []rules.TextEdit {
	var allEdits []rules.TextEdit
	var sequence runSequence
	var sequenceMounts []*instructions.Mount

	flush := func() {
		if edit := r.createSequenceEdit(sequence, data, file, sm); edit != nil {
			allEdits = append(allEdits, *edit)
		}
		sequence = runSequence{}
	}

	for _, cmd := range stage.Commands {
		if _, ok := cmd.(*instructions.ShellCommand); ok {
			flush()
			sequenceMounts = nil
			continue
		}

		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			flush()
			sequenceMounts = nil
			continue
		}

		runVariant := shellVariantAtCommand(cmd, stageInfo, data.ShellVariant)

		// Skip RUNs where the effective shell doesn't support heredoc.
		if !runVariant.SupportsHeredoc() {
			flush()
			sequenceMounts = nil
			continue
		}

		if len(sequence.runs) > 0 && sequence.shellVariant != runVariant {
			flush()
			sequenceMounts = nil
		}

		// Check mount compatibility
		runMounts := runmount.GetMounts(run)
		if len(sequence.runs) > 0 && !runmount.MountsEqual(sequenceMounts, runMounts) {
			flush()
			sequenceMounts = nil
		}

		// Extract commands using the current (per-instruction) variant.
		commands := r.extractCommands(run, runVariant)
		if len(commands) == 0 {
			flush()
			sequenceMounts = nil
			continue
		}

		// Check for exit command (breaks sequence)
		script := r.getRunScript(run)
		if shell.HasExitCommand(script, runVariant) {
			flush()
			sequenceMounts = nil
			continue
		}

		if len(sequence.runs) == 0 {
			sequenceMounts = runMounts
			sequence.shellVariant = runVariant
		}

		sequence.runs = append(sequence.runs, run)
		sequence.commands = append(sequence.commands, commands...)
	}

	// Check final sequence
	flush()

	if len(allEdits) == 0 {
		return nil
	}
	return allEdits
}

// createSequenceEdit creates an edit for a sequence of consecutive RUNs.
// When the sequence has only 1 RUN (e.g., sync fixes broke the consecutive pattern
// by injecting a SHELL instruction), falls back to chained conversion if the
// single RUN has enough commands.
func (r *heredocResolver) createSequenceEdit(
	seq runSequence,
	data *rules.HeredocResolveData,
	file string,
	sm *sourcemap.SourceMap,
) *rules.TextEdit {
	if len(seq.runs) < 2 || len(seq.commands) < data.MinCommands {
		// Fallback: a single RUN with enough chained commands can still
		// be converted. This handles sync fixes (e.g., DL4006 SHELL injection)
		// that break the consecutive pattern by inserting non-RUN instructions.
		if len(seq.runs) == 1 && len(seq.commands) >= data.MinCommands {
			return r.createChainedEdit(seq.runs[0], seq.commands, seq.shellVariant, data, file, sm)
		}
		return nil
	}

	// Verify all commands are simple (can be merged) using the
	// shell variant that was active when this sequence was collected.
	for _, run := range seq.runs {
		script := r.getRunScript(run)
		if len(run.Files) > 0 {
			script = run.Files[0].Data
		}
		if !shell.IsSimpleScript(script, seq.shellVariant) {
			return nil
		}
	}

	firstRun := seq.runs[0]
	lastRun := seq.runs[len(seq.runs)-1]

	firstLoc := firstRun.Location()
	lastLoc := lastRun.Location()
	if len(firstLoc) == 0 || len(lastLoc) == 0 {
		return nil
	}

	startLine := firstLoc[0].Start.Line
	endLine := lastLoc[len(lastLoc)-1].End.Line

	// Get mounts from first RUN
	mounts := runmount.GetMounts(firstRun)

	// Emit set -o pipefail when DL4006 is enabled and commands contain pipes
	pipefail := data.PipefailEnabled && commandsHavePipes(seq.commands, seq.shellVariant)

	// Build heredoc using the shell variant that was active for this sequence
	heredocText := heredoc.FormatWithMounts(seq.commands, mounts, seq.shellVariant, pipefail)

	// Calculate and apply indentation
	indent := extractIndent(sm, startLine)
	heredocText = applyIndent(heredocText, indent)

	return &rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine, 0, endLine, len(sm.Line(endLine-1))),
		NewText:  indent + heredocText,
	}
}

// createChainedEdit creates an edit for a single RUN with chained commands.
// This is used both as a fallback from createSequenceEdit and by detectAndFixChained.
func (r *heredocResolver) createChainedEdit(
	run *instructions.RunCommand,
	commands []string,
	variant shell.Variant,
	data *rules.HeredocResolveData,
	file string,
	sm *sourcemap.SourceMap,
) *rules.TextEdit {
	script := r.getRunScript(run)
	if !shell.IsSimpleScript(script, variant) {
		return nil
	}

	runLoc := run.Location()
	if len(runLoc) == 0 {
		return nil
	}

	startLine := runLoc[0].Start.Line
	endLine := runLoc[len(runLoc)-1].End.Line

	mounts := runmount.GetMounts(run)
	pipefail := data.PipefailEnabled && commandsHavePipes(commands, variant)
	heredocText := heredoc.FormatWithMounts(commands, mounts, variant, pipefail)

	indent := extractIndent(sm, startLine)
	heredocText = applyIndent(heredocText, indent)

	return &rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine, 0, endLine, len(sm.Line(endLine-1))),
		NewText:  indent + heredocText,
	}
}

// detectAndFixChained finds a RUN with chained commands and returns a fix.
func (r *heredocResolver) detectAndFixChained(
	stage instructions.Stage,
	stageInfo *semantic.StageInfo,
	data *rules.HeredocResolveData,
	file string,
	sm *sourcemap.SourceMap,
) []rules.TextEdit {
	for _, cmd := range stage.Commands {
		if _, ok := cmd.(*instructions.ShellCommand); ok {
			continue
		}

		runVariant := shellVariantAtCommand(cmd, stageInfo, data.ShellVariant)

		// Skip instructions where the effective shell doesn't support heredoc.
		if !runVariant.SupportsHeredoc() {
			continue
		}

		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			continue
		}

		// Skip heredoc RUNs (already in preferred syntax)
		if len(run.Files) > 0 {
			continue
		}

		script := r.getRunScript(run)
		if script == "" {
			continue
		}

		commands := shell.ExtractChainedCommands(script, runVariant)
		if len(commands) < data.MinCommands {
			continue
		}

		if edit := r.createChainedEdit(run, commands, runVariant, data, file, sm); edit != nil {
			return []rules.TextEdit{*edit}
		}
	}

	return nil
}

// extractCommands extracts commands from a RUN instruction.
func (r *heredocResolver) extractCommands(run *instructions.RunCommand, shellVariant shell.Variant) []string {
	if len(run.Files) > 0 {
		// Heredoc RUN
		if run.Files[0].Data == "" {
			return nil
		}
		script := run.Files[0].Data
		if !shell.IsSimpleScript(script, shellVariant) {
			return nil // Complex heredoc - can't merge
		}
		return shell.ExtractChainedCommands(script, shellVariant)
	}

	script := r.getRunScript(run)
	if script == "" {
		return nil
	}

	if !shell.IsSimpleScript(script, shellVariant) {
		return nil
	}

	return shell.ExtractChainedCommands(script, shellVariant)
}

// getRunScript extracts the shell script from a RUN instruction.
// For heredoc RUNs, returns the heredoc content. For regular RUNs, returns CmdLine.
func (r *heredocResolver) getRunScript(run *instructions.RunCommand) string {
	// Prefer heredoc content when present - important for detecting exit commands
	// that would break merging semantics
	if len(run.Files) > 0 && run.Files[0].Data != "" {
		return run.Files[0].Data
	}
	if len(run.CmdLine) > 0 {
		return strings.Join(run.CmdLine, " ")
	}
	return ""
}

// commandsHavePipes checks if any command in the list contains a pipe operator.
// Used to decide whether to emit "set -o pipefail" in the heredoc body.
func commandsHavePipes(commands []string, variant shell.Variant) bool {
	for _, cmd := range commands {
		if shell.HasPipes(cmd, variant) {
			return true
		}
	}
	return false
}

func shellVariantAtCommand(cmd instructions.Command, stageInfo *semantic.StageInfo, fallback shell.Variant) shell.Variant {
	if stageInfo == nil {
		return fallback
	}
	locs := cmd.Location()
	if len(locs) == 0 || locs[0].Start.Line <= 0 {
		return stageInfo.ShellSetting.Variant
	}
	return stageInfo.ShellVariantAtLine(locs[0].Start.Line)
}

// extractIndent extracts leading whitespace from a line.
func extractIndent(sm *sourcemap.SourceMap, line int) string {
	if line <= 0 || line > sm.LineCount() {
		return ""
	}
	lineContent := sm.Line(line - 1)
	var indent strings.Builder
	for _, ch := range lineContent {
		if ch == ' ' || ch == '\t' {
			indent.WriteRune(ch)
		} else {
			break
		}
	}
	return indent.String()
}

// applyIndent applies leading indentation to all lines except the first.
// This preserves the visual hierarchy when Dockerfiles use indentation
// for multi-stage builds or readability.
//
// When indent contains tabs, the heredoc operator is converted from << to <<-
// so that BuildKit strips the leading tabs at execution time. Body lines get
// an extra tab for visual nesting under the instruction.
func applyIndent(heredocText, indent string) string {
	if indent == "" {
		return heredocText
	}

	lines := strings.Split(heredocText, "\n")

	bodyPrefix := indent
	if strings.Contains(indent, "\t") {
		// Convert << to <<- so tabs are stripped at execution time.
		if idx := strings.Index(lines[0], "<<"); idx >= 0 {
			if idx+2 >= len(lines[0]) || lines[0][idx+2] != '-' {
				lines[0] = lines[0][:idx+2] + "-" + lines[0][idx+2:]
			}
		}
		// Body lines get an extra tab for visual nesting under the instruction.
		bodyPrefix = indent + "\t"
	}

	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = bodyPrefix + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// init registers the heredoc resolver.
func init() {
	RegisterResolver(&heredocResolver{})
}
