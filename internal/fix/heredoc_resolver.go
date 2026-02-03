// Package fix provides fix application and resolution.
package fix

import (
	"bytes"
	"context"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/heredoc"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/runmount"
	"github.com/tinovyatkin/tally/internal/shell"
	"github.com/tinovyatkin/tally/internal/sourcemap"
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

	// Parse the modified content
	dockerfile, err := parser.Parse(bytes.NewReader(resolveCtx.Content))
	if err != nil {
		return nil, nil //nolint:nilerr // Skip silently - don't fail fix process
	}

	stages, _, err := instructions.Parse(dockerfile.AST, nil)
	if err != nil {
		return nil, nil //nolint:nilerr // Skip silently - don't fail fix process
	}

	// Validate stage index
	if data.StageIndex >= len(stages) {
		return nil, nil
	}
	stage := stages[data.StageIndex]

	// Create sourcemap for position calculations
	sm := sourcemap.New(resolveCtx.Content)

	// Re-run detection based on fix type
	switch data.Type {
	case rules.HeredocFixConsecutive:
		return r.detectAndFixConsecutive(stage, data, resolveCtx.FilePath, sm), nil
	case rules.HeredocFixChained:
		return r.detectAndFixChained(stage, data, resolveCtx.FilePath, sm), nil
	default:
		return nil, nil
	}
}

// runSequence holds a sequence of consecutive RUN instructions.
type runSequence struct {
	runs     []*instructions.RunCommand
	commands []string
}

// detectAndFixConsecutive finds consecutive RUN sequences and returns a fix for the first one found.
func (r *heredocResolver) detectAndFixConsecutive(
	stage instructions.Stage,
	data *rules.HeredocResolveData,
	file string,
	sm *sourcemap.SourceMap,
) []rules.TextEdit {
	var sequence runSequence
	var sequenceMounts []*instructions.Mount

	flushAndCheck := func() []rules.TextEdit {
		if edit := r.createSequenceEdit(sequence, data, file, sm); edit != nil {
			return []rules.TextEdit{*edit}
		}
		sequence = runSequence{}
		return nil
	}

	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			if edits := flushAndCheck(); edits != nil {
				return edits
			}
			sequenceMounts = nil
			continue
		}

		// Check mount compatibility
		runMounts := runmount.GetMounts(run)
		if len(sequence.runs) > 0 && !runmount.MountsEqual(sequenceMounts, runMounts) {
			if edits := flushAndCheck(); edits != nil {
				return edits
			}
			sequenceMounts = nil
		}

		// Extract commands
		commands := r.extractCommands(run, data.ShellVariant)
		if len(commands) == 0 {
			if edits := flushAndCheck(); edits != nil {
				return edits
			}
			sequenceMounts = nil
			continue
		}

		// Check for exit command (breaks sequence)
		script := r.getRunScript(run)
		if shell.HasExitCommand(script, data.ShellVariant) {
			if edits := flushAndCheck(); edits != nil {
				return edits
			}
			sequenceMounts = nil
			continue
		}

		if len(sequence.runs) == 0 {
			sequenceMounts = runMounts
		}

		sequence.runs = append(sequence.runs, run)
		sequence.commands = append(sequence.commands, commands...)
	}

	// Check final sequence
	if edits := flushAndCheck(); edits != nil {
		return edits
	}

	return nil
}

// createSequenceEdit creates an edit for a sequence of consecutive RUNs.
func (r *heredocResolver) createSequenceEdit(
	seq runSequence,
	data *rules.HeredocResolveData,
	file string,
	sm *sourcemap.SourceMap,
) *rules.TextEdit {
	if len(seq.runs) < 2 || len(seq.commands) < data.MinCommands {
		return nil
	}

	// Verify all commands are simple (can be merged)
	for _, run := range seq.runs {
		script := r.getRunScript(run)
		if len(run.Files) > 0 {
			script = run.Files[0].Data
		}
		if !shell.IsSimpleScript(script, data.ShellVariant) {
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

	// Build heredoc
	heredocText := heredoc.FormatWithMounts(seq.commands, mounts, data.ShellVariant)

	// Calculate and apply indentation
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
	data *rules.HeredocResolveData,
	file string,
	sm *sourcemap.SourceMap,
) []rules.TextEdit {
	for _, cmd := range stage.Commands {
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

		commands := shell.ExtractChainedCommands(script, data.ShellVariant)
		if len(commands) < data.MinCommands {
			continue
		}

		// Must be simple to convert
		if !shell.IsSimpleScript(script, data.ShellVariant) {
			continue
		}

		runLoc := run.Location()
		if len(runLoc) == 0 {
			continue
		}

		startLine := runLoc[0].Start.Line
		endLine := runLoc[len(runLoc)-1].End.Line

		mounts := runmount.GetMounts(run)
		heredocText := heredoc.FormatWithMounts(commands, mounts, data.ShellVariant)

		indent := extractIndent(sm, startLine)
		heredocText = applyIndent(heredocText, indent)

		return []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, startLine, 0, endLine, len(sm.Line(endLine-1))),
			NewText:  indent + heredocText,
		}}
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
func applyIndent(heredocText, indent string) string {
	lines := strings.Split(heredocText, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// init registers the heredoc resolver.
func init() {
	RegisterResolver(&heredocResolver{})
}
