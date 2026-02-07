package fix

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/rules"
)

// normalizePath ensures consistent path format for map lookups.
// This handles Windows vs Unix path separator differences.
func normalizePath(path string) string {
	return filepath.Clean(path)
}

// Fixer applies suggested fixes to source files.
type Fixer struct {
	// SafetyThreshold determines the minimum safety level for fixes.
	// Only fixes with Safety <= SafetyThreshold will be applied.
	SafetyThreshold FixSafety

	// RuleFilter limits fixes to specific rule codes.
	// If empty, all rules are eligible.
	RuleFilter []string

	// FixModes maps file paths to their per-rule fix modes.
	// Outer key is the normalized file path, inner key is the rule code.
	// Uses config.FixMode constants (FixModeAlways, FixModeNever, etc.).
	// If nil or a file/rule is not present, FixModeAlways is assumed.
	FixModes map[string]map[string]FixMode

	// Concurrency sets the number of parallel async resolutions.
	// Defaults to 4 if not set.
	Concurrency int
}

// Result contains the outcome of applying fixes.
type Result struct {
	// Changes contains modifications for each file.
	Changes map[string]*FileChange
}

// TotalApplied returns the total number of fixes applied across all files.
func (r *Result) TotalApplied() int {
	count := 0
	for _, fc := range r.Changes {
		count += len(fc.FixesApplied)
	}
	return count
}

// TotalSkipped returns the total number of fixes skipped across all files.
func (r *Result) TotalSkipped() int {
	count := 0
	for _, fc := range r.Changes {
		count += len(fc.FixesSkipped)
	}
	return count
}

// FilesModified returns the number of files with actual changes.
func (r *Result) FilesModified() int {
	count := 0
	for _, fc := range r.Changes {
		if fc.HasChanges() {
			count++
		}
	}
	return count
}

// Apply processes violations and applies their suggested fixes.
// sources maps file paths to their original content.
//
// Fix application follows a two-phase approach to avoid position drift:
//  1. Apply sync fixes first (NeedsResolve=false) - these have pre-computed edits
//  2. Resolve and apply async fixes (NeedsResolve=true) - resolvers see the modified content
//
// This ensures structural transforms (like prefer-run-heredoc) operate on content
// that has already been modified by content fixes (like apt → apt-get).
func (f *Fixer) Apply(ctx context.Context, violations []rules.Violation, sources map[string][]byte) (*Result, error) {
	result := &Result{
		Changes: make(map[string]*FileChange),
	}

	// Initialize FileChange for each source file
	f.initializeChanges(result, sources)

	// Classify violations into sync and async candidates
	syncCandidates, asyncCandidates := f.classifyViolations(violations, result.Changes)

	// Phase 1: Apply sync fixes (content fixes with pre-computed edits)
	f.applyCandidatesToFiles(result.Changes, syncCandidates)

	// Phase 2: Resolve and apply async fixes (each is applied immediately after resolution)
	if len(asyncCandidates) > 0 {
		f.resolveAsyncFixes(ctx, result.Changes, asyncCandidates)
		// Record skipped fixes for any that still need resolution (resolver failed)
		for _, fc := range asyncCandidates {
			if fc.fix.NeedsResolve {
				recordSkipped(result.Changes, fc.violation, SkipResolveError, "resolver failed or missing")
			}
		}
	}

	return result, nil
}

// initializeChanges populates the result with FileChange entries for each source file.
func (f *Fixer) initializeChanges(result *Result, sources map[string][]byte) {
	for path, content := range sources {
		normalizedPath := normalizePath(path)
		result.Changes[normalizedPath] = &FileChange{
			Path:            path,
			OriginalContent: content,
			ModifiedContent: bytes.Clone(content),
		}
	}
}

// classifyViolations separates violations into sync and async fix candidates.
func (f *Fixer) classifyViolations(violations []rules.Violation, changes map[string]*FileChange) ([]*fixCandidate, []*fixCandidate) {
	syncCandidates := make([]*fixCandidate, 0, len(violations))
	var asyncCandidates []*fixCandidate

	for i := range violations {
		v := &violations[i]
		if v.SuggestedFix == nil {
			continue
		}

		if !f.ruleAllowed(v.RuleCode) {
			recordSkipped(changes, v, SkipRuleFilter, "")
			continue
		}
		if v.SuggestedFix.Safety > f.SafetyThreshold {
			recordSkipped(changes, v, SkipSafety, "")
			continue
		}
		if !f.fixModeAllowed(v.File(), v.RuleCode) {
			recordSkipped(changes, v, SkipFixMode, "")
			continue
		}

		candidate := &fixCandidate{violation: v, fix: v.SuggestedFix}
		if v.SuggestedFix.NeedsResolve {
			asyncCandidates = append(asyncCandidates, candidate)
		} else {
			syncCandidates = append(syncCandidates, candidate)
		}
	}

	return syncCandidates, asyncCandidates
}

// applyCandidatesToFiles groups candidates by file and applies them.
func (f *Fixer) applyCandidatesToFiles(changes map[string]*FileChange, candidates []*fixCandidate) {
	byFile := make(map[string][]*fixCandidate)
	for _, fc := range candidates {
		if len(fc.fix.Edits) == 0 {
			recordSkipped(changes, fc.violation, SkipNoEdits, "")
			continue
		}
		normalizedFile := normalizePath(fc.violation.File())
		byFile[normalizedFile] = append(byFile[normalizedFile], fc)
	}

	for file, fileCandidates := range byFile {
		fc := changes[file]
		if fc == nil {
			continue
		}
		f.applyFixesToFile(fc, fileCandidates)
	}
}

// fixCandidate pairs a violation with its suggested fix for processing.
type fixCandidate struct {
	violation *rules.Violation
	fix       *rules.SuggestedFix
}

// recordSkipped adds a skipped fix entry for a file if the file exists in changes.
func recordSkipped(changes map[string]*FileChange, v *rules.Violation, reason SkipReason, errMsg string) {
	if fc := changes[normalizePath(v.File())]; fc != nil {
		skipped := SkippedFix{
			RuleCode: v.RuleCode,
			Reason:   reason,
			Location: v.Location,
		}
		if errMsg != "" {
			skipped.Error = errMsg
		}
		fc.FixesSkipped = append(fc.FixesSkipped, skipped)
	}
}

// ruleAllowed checks if a rule passes the filter.
func (f *Fixer) ruleAllowed(ruleCode string) bool {
	if len(f.RuleFilter) == 0 {
		return true
	}
	return slices.Contains(f.RuleFilter, ruleCode)
}

// fixModeAllowed checks if a fix is allowed based on the file's per-rule fix mode config.
// Returns true if the fix should be applied.
func (f *Fixer) fixModeAllowed(filePath, ruleCode string) bool {
	mode := config.FixModeAlways // default
	if f.FixModes != nil {
		normalizedPath := normalizePath(filePath)
		if fileModes, ok := f.FixModes[normalizedPath]; ok {
			if m, ok := fileModes[ruleCode]; ok {
				mode = m
			}
		}
	}

	switch mode {
	case config.FixModeNever:
		// Never apply fixes for this rule
		return false
	case config.FixModeExplicit:
		// Only apply if rule is in --fix-rule list
		return len(f.RuleFilter) > 0 && slices.Contains(f.RuleFilter, ruleCode)
	case config.FixModeUnsafeOnly:
		// Only apply if --fix-unsafe was used (SafetyThreshold >= FixUnsafe)
		return f.SafetyThreshold >= rules.FixUnsafe
	case config.FixModeAlways:
		// Always apply (safety already checked)
		return true
	default:
		// Unknown mode, treat as always
		return true
	}
}

// resolveAsyncFixes runs resolvers for fixes that need external data.
// This is called AFTER sync fixes have been applied, so resolvers receive
// the modified content and can compute correct positions.
//
// IMPORTANT: Async fixes are resolved and applied ONE AT A TIME, sequentially.
// This ensures each resolver sees the content after previous async fixes were applied,
// avoiding position drift between async fixes.
func (f *Fixer) resolveAsyncFixes(ctx context.Context, changes map[string]*FileChange, candidates []*fixCandidate) {
	for _, candidate := range candidates {
		fix := candidate.fix
		if !fix.NeedsResolve {
			continue
		}

		resolver := GetResolver(fix.ResolverID)
		if resolver == nil {
			// Unknown resolver, will be skipped later
			continue
		}

		// Get the CURRENT content (may have been modified by previous async fixes)
		normalizedFile := normalizePath(candidate.violation.File())
		fc := changes[normalizedFile]
		if fc == nil {
			continue
		}

		resolveCtx := ResolveContext{
			FilePath: fc.Path,
			Content:  fc.ModifiedContent,
		}

		// Resolve synchronously (sequential to avoid position drift between async fixes)
		edits, err := resolver.Resolve(ctx, resolveCtx, fix)
		if err != nil {
			// Mark as failed but continue with other fixes
			continue
		}
		fix.Edits = edits
		fix.NeedsResolve = false

		// Apply this fix immediately so the next resolver sees updated content
		if len(fix.Edits) > 0 {
			f.applyFixesToFile(fc, []*fixCandidate{candidate})
		}
	}
}

// applyFixesToFile applies non-conflicting fixes to a single file.
// Fixes are atomic: either all edits of a fix are applied, or none are.
func (f *Fixer) applyFixesToFile(fc *FileChange, candidates []*fixCandidate) {
	// Collect all edits with their source info
	type editWithSource struct {
		edit      rules.TextEdit
		candidate *fixCandidate
	}

	var allEdits []editWithSource
	candidateEdits := make(map[*fixCandidate][]rules.TextEdit, len(candidates))
	for _, c := range candidates {
		candidateEdits[c] = c.fix.Edits
		for _, edit := range c.fix.Edits {
			allEdits = append(allEdits, editWithSource{
				edit:      edit,
				candidate: c,
			})
		}
	}

	// Sort edits by priority first (lower = earlier), then by position (descending).
	// This ensures content fixes (priority 0) run before structural transforms (priority 100+),
	// and within the same priority, later positions are processed first to handle position drift.
	sort.Slice(allEdits, func(i, j int) bool {
		iPriority := allEdits[i].candidate.fix.Priority
		jPriority := allEdits[j].candidate.fix.Priority
		if iPriority != jPriority {
			return iPriority < jPriority
		}
		// Within same priority, later positions first (existing behavior)
		return !compareEdits(allEdits[i].edit, allEdits[j].edit)
	})

	// Track which candidates have been applied or skipped
	applied := make(map[*fixCandidate]bool)
	skipped := make(map[*fixCandidate]bool)
	checked := make(map[*fixCandidate]bool)

	// hasCandidateConflict checks if any of a candidate's edits overlap with reserved edits
	hasCandidateConflict := func(edits []rules.TextEdit, reservedEdits []editWithSource) bool {
		for _, e := range edits {
			for _, re := range reservedEdits {
				if editsOverlap(e, re.edit) {
					return true
				}
			}
		}
		return false
	}

	// Apply edits, checking for conflicts at the fix level (atomic)
	// reservedEdits tracks ALL edits from approved candidates (for conflict detection)
	// This ensures that when candidate A is approved, all of A's edits are reserved
	// before checking candidate B, even if some of A's edits haven't been applied yet.
	content := fc.ModifiedContent
	reservedEdits := make([]editWithSource, 0, len(allEdits))

	// Track column shifts caused by applied edits for cross-priority position adjustment.
	// When edits from different priority groups modify the same line, earlier-priority edits
	// change column positions. Without adjustment, later-priority edits use stale positions.
	var colShifts []columnShift

	for _, ews := range allEdits {
		// Skip if this candidate was already determined to be skipped
		if skipped[ews.candidate] {
			continue
		}

		// Check all edits for this candidate on first encounter
		if !checked[ews.candidate] {
			checked[ews.candidate] = true
			if hasCandidateConflict(candidateEdits[ews.candidate], reservedEdits) {
				skipped[ews.candidate] = true
				fc.FixesSkipped = append(fc.FixesSkipped, SkippedFix{
					RuleCode: ews.candidate.violation.RuleCode,
					Reason:   SkipConflict,
					Location: ews.candidate.violation.Location,
				})
				continue
			}
			// Reserve ALL edits from this candidate immediately to prevent
			// interleaving conflicts with candidates checked later
			for _, edit := range candidateEdits[ews.candidate] {
				reservedEdits = append(reservedEdits, editWithSource{
					edit:      edit,
					candidate: ews.candidate,
				})
			}
			applied[ews.candidate] = true
		}

		// Adjust the edit's positions based on column shifts from prior edits
		edit := ews.edit
		adjustEditColumns(&edit, colShifts)

		// Apply the adjusted edit
		content = applyEdit(content, edit)

		// Record column shift for single-line edits without newlines in replacement.
		// Multi-line edits or edits producing newlines change line structure;
		// those are handled by async resolvers which re-parse the content.
		recordColumnShift(&colShifts, ews.edit)
	}

	fc.ModifiedContent = content

	// Record applied fixes with their original edits.
	for c := range applied {
		fc.FixesApplied = append(fc.FixesApplied, AppliedFix{
			RuleCode:    c.violation.RuleCode,
			Description: c.fix.Description,
			Location:    c.violation.Location,
			Edits:       c.fix.Edits,
		})
	}
}

// columnShift records a column offset caused by an applied edit on a single line.
type columnShift struct {
	line     int // 1-based line number where the shift applies
	afterCol int // original column at or after which positions shift
	delta    int // number of columns shifted (positive = content grew)
}

// adjustEditColumns adjusts an edit's start/end columns based on accumulated column shifts.
// Uses the original (pre-adjustment) coordinate space: for each shift, positions at or
// past the shift's afterCol get shifted by delta.
func adjustEditColumns(edit *rules.TextEdit, shifts []columnShift) {
	startDelta, endDelta := 0, 0
	for _, s := range shifts {
		if s.line == edit.Location.Start.Line && edit.Location.Start.Column >= s.afterCol {
			startDelta += s.delta
		}
		if s.line == edit.Location.End.Line && edit.Location.End.Column >= s.afterCol {
			endDelta += s.delta
		}
	}
	edit.Location.Start.Column += startDelta
	edit.Location.End.Column += endDelta
}

// recordColumnShift records a column shift from a single-line edit that doesn't introduce newlines.
// Uses the ORIGINAL (unadjusted) edit positions to keep all shifts in the same coordinate space.
func recordColumnShift(shifts *[]columnShift, edit rules.TextEdit) {
	if edit.Location.Start.Line != edit.Location.End.Line {
		return // Multi-line edit: line structure changes, can't track as column shift
	}
	if strings.Contains(edit.NewText, "\n") {
		return // Replacement introduces new lines: line structure changes
	}

	oldLen := edit.Location.End.Column - edit.Location.Start.Column
	newLen := len(edit.NewText) // byte length — columns are byte offsets in this codebase
	delta := newLen - oldLen
	if delta == 0 {
		return // Same-length replacement (e.g., casing fix): no shift
	}

	*shifts = append(*shifts, columnShift{
		line:     edit.Location.Start.Line,
		afterCol: edit.Location.End.Column, // positions at or past the edit's end shift
		delta:    delta,
	})
}

// applyEdit applies a single text edit to content.
// The edit replaces the range [Start, End) with NewText.
// Location uses 1-based line numbers (BuildKit convention); we convert to 0-based for array indexing.
func applyEdit(content []byte, edit rules.TextEdit) []byte {
	// Detect line ending style (CRLF on Windows, LF on Unix)
	lineEnding := []byte("\n")
	if bytes.Contains(content, []byte("\r\n")) {
		lineEnding = []byte("\r\n")
	}

	// Split by the detected line ending
	lines := bytes.Split(content, lineEnding)

	// Convert from 1-based (Location) to 0-based (array indexing)
	startLine := edit.Location.Start.Line - 1
	startCol := edit.Location.Start.Column
	endLine := edit.Location.End.Line - 1
	endCol := edit.Location.End.Column

	// Validate line indices (now 0-based)
	if startLine < 0 || startLine >= len(lines) {
		return content
	}
	if endLine < 0 || endLine >= len(lines) {
		return content
	}

	// Clamp column values
	if startCol < 0 {
		startCol = 0
	}
	if startCol > len(lines[startLine]) {
		startCol = len(lines[startLine])
	}
	if endCol < 0 {
		endCol = 0
	}
	if endCol > len(lines[endLine]) {
		endCol = len(lines[endLine])
	}

	// Build the new content
	var result bytes.Buffer

	// Lines before the edit
	for i := range startLine {
		result.Write(lines[i])
		result.Write(lineEnding)
	}

	// Start line up to the edit start
	result.Write(lines[startLine][:startCol])

	// Normalize newlines in replacement text to match file's line ending style
	newText := edit.NewText
	if !bytes.Equal(lineEnding, []byte("\n")) {
		// File uses CRLF, normalize any LF-only to CRLF
		newText = strings.ReplaceAll(newText, "\r\n", "\n") // First normalize to LF
		newText = strings.ReplaceAll(newText, "\n", string(lineEnding))
	}

	// The replacement text
	result.WriteString(newText)

	// End line from the edit end
	result.Write(lines[endLine][endCol:])

	// Lines after the edit
	for i := endLine + 1; i < len(lines); i++ {
		result.Write(lineEnding)
		result.Write(lines[i])
	}

	return result.Bytes()
}
