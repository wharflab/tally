// Package fix provides auto-fix infrastructure for tally.
// It includes types for fix safety levels, fix resolution, and fix application.
package fix

import (
	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/rules"
)

// Re-export FixSafety from rules package for convenience.
// This allows fix package users to use fix.FixSafe instead of rules.FixSafe.
type FixSafety = rules.FixSafety

const (
	// FixSafe means the fix is always correct and won't change behavior.
	FixSafe = rules.FixSafe

	// FixSuggestion means the fix is likely correct but may need review.
	FixSuggestion = rules.FixSuggestion

	// FixUnsafe means the fix might change behavior significantly.
	FixUnsafe = rules.FixUnsafe
)

// Re-export FixMode from config for convenience.
type FixMode = config.FixMode

const (
	// FixModeNever disables fixes even with --fix.
	FixModeNever = config.FixModeNever

	// FixModeExplicit requires --fix-rule to apply.
	FixModeExplicit = config.FixModeExplicit

	// FixModeAlways applies with --fix when safety threshold is met (default).
	FixModeAlways = config.FixModeAlways

	// FixModeUnsafeOnly requires --fix-unsafe to apply.
	FixModeUnsafeOnly = config.FixModeUnsafeOnly
)

// AppliedFix records a successfully applied fix.
type AppliedFix struct {
	// RuleCode identifies which rule this fix is for.
	RuleCode string

	// Description explains what the fix did.
	Description string

	// Location is where the fix was applied.
	Location rules.Location

	// Edits are the original (pre-adjustment) text edits of this fix.
	// Positions reference the original document content, making them
	// suitable for direct conversion to LSP TextEdits.
	Edits []rules.TextEdit
}

// SkipReason explains why a fix was skipped.
type SkipReason int

const (
	// SkipConflict means the fix overlaps with another fix.
	SkipConflict SkipReason = iota

	// SkipSafety means the fix is below the safety threshold.
	SkipSafety

	// SkipRuleFilter means the rule is not in the --fix-rule list.
	SkipRuleFilter

	// SkipResolveError means the async resolver failed.
	SkipResolveError

	// SkipNoEdits means the fix has no edits (invalid fix).
	SkipNoEdits

	// SkipFixMode means the rule's fix mode config disallows fixing.
	SkipFixMode
)

// String returns a human-readable description of the skip reason.
func (r SkipReason) String() string {
	switch r {
	case SkipConflict:
		return "conflicts with another fix"
	case SkipSafety:
		return "below safety threshold"
	case SkipRuleFilter:
		return "rule not in fix-rule list"
	case SkipResolveError:
		return "resolver failed"
	case SkipNoEdits:
		return "no edits in fix"
	case SkipFixMode:
		return "disabled by fix mode config"
	default:
		return "unknown reason"
	}
}

// SkippedFix records a fix that couldn't be applied.
type SkippedFix struct {
	// RuleCode identifies which rule this fix is for.
	RuleCode string

	// Reason explains why the fix was skipped.
	Reason SkipReason

	// Location is where the fix would have been applied.
	Location rules.Location

	// Error contains the error message if Reason is SkipResolveError.
	Error string
}

// FileChange describes changes to a single file.
type FileChange struct {
	// Path is the file path.
	Path string

	// FixesApplied lists the fixes that were applied.
	FixesApplied []AppliedFix

	// FixesSkipped lists fixes that couldn't be applied.
	FixesSkipped []SkippedFix

	// OriginalContent is the file content before fixes.
	OriginalContent []byte

	// ModifiedContent is the file content after fixes.
	ModifiedContent []byte
}

// HasChanges returns true if any fixes were applied to this file.
func (fc *FileChange) HasChanges() bool {
	return len(fc.FixesApplied) > 0
}
