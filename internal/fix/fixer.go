package fix

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

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
func (f *Fixer) Apply(ctx context.Context, violations []rules.Violation, sources map[string][]byte) (*Result, error) {
	result := &Result{
		Changes: make(map[string]*FileChange),
	}

	// Initialize FileChange for each source file
	// Use normalized paths as keys for consistent cross-platform lookups
	for path, content := range sources {
		normalizedPath := normalizePath(path)
		result.Changes[normalizedPath] = &FileChange{
			Path:            path, // Keep original path for file operations
			OriginalContent: content,
			ModifiedContent: bytes.Clone(content),
		}
	}

	// Collect fixes that need resolution and those that don't
	var asyncFixes []*rules.SuggestedFix
	syncFixes := make([]*fixCandidate, 0, len(violations)) // Pre-allocate for common case

	for i := range violations {
		v := &violations[i]
		if v.SuggestedFix == nil {
			continue
		}

		// Check rule filter
		if !f.ruleAllowed(v.RuleCode) {
			recordSkipped(result.Changes, v, SkipRuleFilter, "")
			continue
		}

		// Check safety threshold
		if v.SuggestedFix.Safety > f.SafetyThreshold {
			recordSkipped(result.Changes, v, SkipSafety, "")
			continue
		}

		// Check fix mode config (per-file)
		if !f.fixModeAllowed(v.File(), v.RuleCode) {
			recordSkipped(result.Changes, v, SkipFixMode, "")
			continue
		}

		if v.SuggestedFix.NeedsResolve {
			asyncFixes = append(asyncFixes, v.SuggestedFix)
		}

		syncFixes = append(syncFixes, &fixCandidate{
			violation: v,
			fix:       v.SuggestedFix,
		})
	}

	// Resolve async fixes
	if len(asyncFixes) > 0 {
		if err := f.resolveAsyncFixes(ctx, asyncFixes); err != nil {
			return nil, err
		}
	}

	// Group fixes by file
	fixesByFile := make(map[string][]*fixCandidate)
	for _, fc := range syncFixes {
		// Skip fixes that still need resolution (resolver failed or resolver missing)
		if fc.fix.NeedsResolve {
			recordSkipped(result.Changes, fc.violation, SkipResolveError, "resolver failed or missing")
			continue
		}
		// Skip fixes with no edits
		if len(fc.fix.Edits) == 0 {
			recordSkipped(result.Changes, fc.violation, SkipNoEdits, "")
			continue
		}
		normalizedFile := normalizePath(fc.violation.File())
		fixesByFile[normalizedFile] = append(fixesByFile[normalizedFile], fc)
	}

	// Apply fixes to each file
	for file, candidates := range fixesByFile {
		fc := result.Changes[file]
		if fc == nil {
			continue
		}
		f.applyFixesToFile(fc, candidates)
	}

	return result, nil
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
func (f *Fixer) resolveAsyncFixes(ctx context.Context, fixes []*rules.SuggestedFix) error {
	concurrency := f.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for _, fix := range fixes {
		if !fix.NeedsResolve {
			continue
		}

		resolver := GetResolver(fix.ResolverID)
		if resolver == nil {
			// Unknown resolver, will be skipped later
			continue
		}

		// Capture loop variable for goroutine
		g.Go(func() error {
			edits, err := resolver.Resolve(ctx, fix)
			if err != nil {
				// Mark as failed but don't fail the whole operation
				// The fix will be skipped with SkipResolveError
				return nil //nolint:nilerr // Intentionally swallowing error
			}
			fix.Edits = edits
			fix.NeedsResolve = false
			return nil
		})
	}

	return g.Wait()
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

	// Sort edits by position (descending - apply from end to start)
	sort.Slice(allEdits, func(i, j int) bool {
		// Reverse order: later positions first
		return !compareEdits(allEdits[i].edit, allEdits[j].edit)
	})

	// Track which candidates have been applied or skipped
	applied := make(map[*fixCandidate]bool)
	skipped := make(map[*fixCandidate]bool)
	checked := make(map[*fixCandidate]bool)

	// hasCandidateConflict checks if any of a candidate's edits overlap with applied edits
	hasCandidateConflict := func(edits []rules.TextEdit, appliedEdits []editWithSource) bool {
		for _, e := range edits {
			for _, ae := range appliedEdits {
				if editsOverlap(e, ae.edit) {
					return true
				}
			}
		}
		return false
	}

	// Apply edits, checking for conflicts at the fix level (atomic)
	content := fc.ModifiedContent
	appliedEdits := make([]editWithSource, 0, len(allEdits))

	for _, ews := range allEdits {
		// Skip if this candidate was already determined to be skipped
		if skipped[ews.candidate] {
			continue
		}

		// Check all edits for this candidate on first encounter
		if !checked[ews.candidate] {
			checked[ews.candidate] = true
			if hasCandidateConflict(candidateEdits[ews.candidate], appliedEdits) {
				skipped[ews.candidate] = true
				fc.FixesSkipped = append(fc.FixesSkipped, SkippedFix{
					RuleCode: ews.candidate.violation.RuleCode,
					Reason:   SkipConflict,
					Location: ews.candidate.violation.Location,
				})
				continue
			}
			applied[ews.candidate] = true
		}

		// Apply the edit
		content = applyEdit(content, ews.edit)
		appliedEdits = append(appliedEdits, ews)
	}

	fc.ModifiedContent = content

	// Record applied fixes
	for c := range applied {
		fc.FixesApplied = append(fc.FixesApplied, AppliedFix{
			RuleCode:    c.violation.RuleCode,
			Description: c.fix.Description,
			Location:    c.violation.Location,
		})
	}
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
