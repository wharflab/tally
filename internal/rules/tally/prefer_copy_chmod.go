package tally

import (
	"fmt"
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// PreferCopyChmodRule suggests using COPY --chmod instead of a separate COPY + RUN chmod.
type PreferCopyChmodRule struct{}

// NewPreferCopyChmodRule creates a new prefer-copy-chmod rule instance.
func NewPreferCopyChmodRule() *PreferCopyChmodRule { return &PreferCopyChmodRule{} }

// Metadata returns the rule metadata.
func (r *PreferCopyChmodRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.TallyRulePrefix + "prefer-copy-chmod",
		Name:            "Prefer COPY --chmod over separate RUN chmod",
		Description:     "Use COPY --chmod instead of a separate COPY followed by RUN chmod",
		DocURL:          rules.TallyDocURL(rules.TallyRulePrefix + "prefer-copy-chmod"),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "style",
		FixPriority:     99, // Match prefer-copy-heredoc to avoid cross-priority line drift
	}
}

// Check runs the prefer-copy-chmod rule.
func (r *PreferCopyChmodRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // type assertion OK

	violations := make([]rules.Violation, 0, len(input.Stages))

	for stageIdx, stage := range input.Stages {
		shellVariant := shell.VariantBash
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
			}
		}

		// Don't skip non-POSIX stages entirely — exec-form RUN ["chmod", ...]
		// doesn't need shell parsing. The shell variant is only used to gate
		// shell-form parsing inside detectChmod.
		violations = append(violations, r.checkStage(stage, shellVariant, input.File, sm, meta)...)
	}

	return violations
}

// checkStage checks a single build stage for COPY + RUN chmod pairs.
func (r *PreferCopyChmodRule) checkStage(
	stage instructions.Stage,
	shellVariant shell.Variant,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	var prevCopy *instructions.CopyCommand
	workdir := "/" // Docker default

	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.WorkdirCommand:
			workdir = facts.ResolveWorkdir(workdir, c.Path)
			prevCopy = nil

		case *instructions.CopyCommand:
			prevCopy = nil // reset; evaluate this COPY as a potential candidate
			if isCopyChmodCandidate(c) {
				prevCopy = c
			}

		case *instructions.RunCommand:
			if prevCopy != nil {
				if v := r.checkCopyChmodPair(
					prevCopy, c, shellVariant, workdir, file, sm, meta,
				); v != nil {
					violations = append(violations, *v)
				}
			}
			prevCopy = nil

		default:
			prevCopy = nil
		}
	}

	return violations
}

// isCopyChmodCandidate checks whether a COPY instruction is eligible for the rule.
func isCopyChmodCandidate(c *instructions.CopyCommand) bool {
	// Heredoc COPY (e.g. COPY <<EOF /app/script.sh) — always a single dest file
	if len(c.SourceContents) > 0 {
		return true
	}

	// File-based COPY: must have exactly one source (not glob, not multiple)
	if len(c.SourcePaths) != 1 {
		return false
	}
	if strings.ContainsAny(c.SourcePaths[0], "*?[") {
		return false
	}

	return true
}

// checkCopyChmodPair checks whether a COPY + RUN pair is a COPY followed by chmod.
func (r *PreferCopyChmodRule) checkCopyChmodPair(
	copyCmd *instructions.CopyCommand,
	runCmd *instructions.RunCommand,
	shellVariant shell.Variant,
	workdir, file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.Violation {
	chmodInfo := detectChmod(runCmd, shellVariant)
	if chmodInfo == nil {
		return nil
	}

	// Only match absolute chmod targets
	if !path.IsAbs(chmodInfo.Target) {
		return nil
	}

	// Match chmod target to COPY effective destination
	effectiveDest := effectiveCopyDest(copyCmd, workdir)
	if effectiveDest == "" || effectiveDest != chmodInfo.Target {
		return nil
	}

	// Compute the final mode string for the --chmod flag
	finalMode := chmodInfo.RawMode
	if copyCmd.Chmod != "" {
		merged, ok := shell.MergeChmodModes(copyCmd.Chmod, chmodInfo.RawMode)
		if !ok {
			return nil // can't merge — skip
		}
		finalMode = merged
	}

	copyLoc := copyCmd.Location()
	if len(copyLoc) == 0 {
		return nil
	}

	loc := rules.NewLocationFromRanges(file, copyLoc)

	v := rules.NewViolation(
		loc,
		meta.Code,
		fmt.Sprintf("use COPY --chmod=%s instead of separate COPY + RUN chmod", finalMode),
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		fmt.Sprintf("Merge COPY and RUN chmod into a single COPY --chmod=%s instruction for fewer layers", finalMode),
	)

	if fix := r.buildFix(copyCmd, finalMode, runCmd, file, sm, meta); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

// effectiveCopyDest resolves the effective destination path for a COPY instruction.
// Relative destinations are resolved against the stage's effective workdir.
// For heredoc COPY the dest is used directly; for file COPY directory destinations
// are resolved by appending the source basename.
func effectiveCopyDest(c *instructions.CopyCommand, workdir string) string {
	rawDest := c.DestPath
	dest := rawDest

	// Resolve relative destination against WORKDIR
	if !path.IsAbs(dest) {
		dest = path.Join(workdir, dest)
	}

	// Heredoc COPY: dest is always the target file path directly
	if len(c.SourceContents) > 0 {
		return path.Clean(dest)
	}

	// File-based COPY: need exactly one source to resolve
	if len(c.SourcePaths) != 1 {
		return ""
	}

	// Determine if the destination is a directory: explicit trailing slash,
	// or relative "." / ".." which always refer to directories.
	isDir := strings.HasSuffix(rawDest, "/") ||
		path.Clean(rawDest) == "." || path.Clean(rawDest) == ".."
	if isDir {
		dest = path.Join(dest, path.Base(c.SourcePaths[0]))
	}

	return path.Clean(dest)
}

// buildFix creates a two-edit fix: modify COPY's --chmod and delete the RUN chmod.
// finalMode is the resolved mode string for the resulting --chmod flag.
func (r *PreferCopyChmodRule) buildFix(
	copyCmd *instructions.CopyCommand,
	finalMode string,
	runCmd *instructions.RunCommand,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	copyLoc := copyCmd.Location()
	runLoc := runCmd.Location()
	if len(copyLoc) == 0 || len(runLoc) == 0 {
		return nil
	}

	copyLine := copyLoc[0].Start.Line

	// Edit 1: Either insert a new --chmod flag or replace the existing value.
	var edit1 rules.TextEdit
	if copyCmd.Chmod == "" {
		// No existing --chmod → insert after "COPY "
		insertCol := findCopyInsertCol(sm, copyLine)
		edit1 = rules.TextEdit{
			Location: rules.NewRangeLocation(file, copyLine, insertCol, copyLine, insertCol),
			NewText:  "--chmod=" + finalMode + " ",
		}
	} else {
		// Existing --chmod → find and replace the value
		edit1 = buildChmodValueReplaceEdit(sm, file, copyLine, copyCmd.Chmod, finalMode)
	}

	// Edit 2: Delete the entire RUN chmod line(s).
	runStartLine := runLoc[0].Start.Line
	runEndLine, runEndCol := resolveRunEndPosition(runLoc, sm, runCmd)

	var edit2 rules.TextEdit
	if sm != nil && runEndLine >= sm.LineCount() {
		edit2 = rules.TextEdit{
			Location: rules.NewRangeLocation(file, runStartLine, 0, runEndLine, runEndCol),
			NewText:  "",
		}
	} else {
		edit2 = rules.TextEdit{
			Location: rules.NewRangeLocation(file, runStartLine, 0, runEndLine+1, 0),
			NewText:  "",
		}
	}

	return &rules.SuggestedFix{
		Description: "Merge COPY + RUN chmod into COPY --chmod=" + finalMode,
		Safety:      rules.FixSafe,
		Priority:    meta.FixPriority,
		Edits:       []rules.TextEdit{edit1, edit2},
	}
}

// buildChmodValueReplaceEdit creates a TextEdit that replaces an existing --chmod value.
// It finds "--chmod=<oldValue>" on the COPY line and replaces the value portion.
func buildChmodValueReplaceEdit(sm *sourcemap.SourceMap, file string, line int, oldValue, newValue string) rules.TextEdit {
	needle := "--chmod=" + oldValue
	replacement := "--chmod=" + newValue

	if sm != nil {
		lineText := sm.Line(line - 1) // 0-based
		if idx := strings.Index(lineText, needle); idx >= 0 {
			return rules.TextEdit{
				Location: rules.NewRangeLocation(file, line, idx, line, idx+len(needle)),
				NewText:  replacement,
			}
		}
	}

	// Fallback: zero-width insertion won't work, but shouldn't reach here
	return rules.TextEdit{
		Location: rules.NewRangeLocation(file, line, 0, line, 0),
		NewText:  "",
	}
}

// detectChmod extracts chmod info from a RUN instruction.
// Handles both shell-form (RUN chmod +x /f) and exec-form (RUN ["chmod", "+x", "/f"]).
func detectChmod(run *instructions.RunCommand, shellVariant shell.Variant) *shell.ChmodInfo {
	if run.PrependShell {
		// Shell-form needs POSIX AST parsing
		if !shellVariant.SupportsPOSIXShellAST() {
			return nil
		}
		script := getRunCmdLine(run)
		if script == "" {
			return nil
		}
		return shell.DetectStandaloneChmod(script, shellVariant)
	}

	// Exec form: CmdLine is ["chmod", "<mode>", "<target>"]
	if len(run.CmdLine) < 3 || run.CmdLine[0] != "chmod" {
		return nil
	}

	args := run.CmdLine[1:]
	var rawMode, target string
	for _, arg := range args {
		if rawMode == "" {
			// Check mode first — symbolic modes like "-x" start with "-"
			if shell.IsOctalMode(arg) || shell.IsSymbolicMode(arg) {
				rawMode = arg
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return nil // flags like -R not supported
			}
			return nil // first non-flag arg must be the mode
		}
		if target != "" {
			return nil // multiple targets not supported
		}
		target = arg
	}
	if rawMode == "" || target == "" {
		return nil
	}

	var mode uint16
	if shell.IsOctalMode(rawMode) {
		mode = shell.ParseOctalMode(rawMode)
	} else {
		mode = shell.ApplySymbolicMode(rawMode, 0o644)
	}
	if mode == 0 {
		return nil
	}

	return &shell.ChmodInfo{Mode: mode, RawMode: rawMode, Target: target}
}

// copyKeywordLen is the length of "COPY " (keyword + trailing space).
var copyKeywordLen = len(command.Copy) + 1

// findCopyInsertCol finds the column position right after "COPY " in the source line.
// This handles leading whitespace (e.g., " COPY ..." where Start.Character=0).
func findCopyInsertCol(sm *sourcemap.SourceMap, line int) int {
	if sm == nil {
		return copyKeywordLen // fallback
	}
	// SourceMap uses 0-based lines; BuildKit uses 1-based
	lineText := sm.Line(line - 1)
	// Find "COPY " (case-insensitive for robustness)
	upper := strings.ToUpper(lineText)
	keyword := strings.ToUpper(command.Copy) + " "
	idx := strings.Index(upper, keyword)
	if idx >= 0 {
		return idx + copyKeywordLen
	}
	// Fallback: just after "COPY" without space
	idx = strings.Index(upper, strings.ToUpper(command.Copy))
	if idx >= 0 {
		return idx + copyKeywordLen
	}
	return copyKeywordLen
}

func init() {
	rules.Register(NewPreferCopyChmodRule())
}
