package fix

import (
	"path/filepath"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/rules"
)

// FilterFixedViolations removes violations that were fixed and suppresses
// stale violations from rules implementing PostFixRevalidator.
//
// A violation is removed if:
//   - It was directly fixed (exact location+rule match in fixResult), or
//   - The rule implements PostFixRevalidator and RevalidateAfterFix returns
//     false for the modified file content (the fix from another rule
//     resolved the condition).
func FilterFixedViolations(
	violations []rules.Violation,
	fixResult *Result,
	fileConfigs map[string]*config.Config,
) []rules.Violation {
	type locKey struct {
		file string
		line int
		col  int
		code string
	}
	fixed := make(map[locKey]bool)

	modifiedContent := make(map[string][]byte)
	for _, fc := range fixResult.Changes {
		for _, af := range fc.FixesApplied {
			fixed[locKey{
				file: filepath.ToSlash(fc.Path),
				line: af.Location.Start.Line,
				col:  af.Location.Start.Column,
				code: af.RuleCode,
			}] = true
		}
		if fc.ModifiedContent != nil {
			modifiedContent[filepath.ToSlash(fc.Path)] = fc.ModifiedContent
		}
	}

	remaining := make([]rules.Violation, 0, len(violations))
	for _, v := range violations {
		key := locKey{
			file: filepath.ToSlash(v.File()),
			line: v.Line(),
			col:  v.Location.Start.Column,
			code: v.RuleCode,
		}
		if fixed[key] {
			continue
		}

		if content, ok := modifiedContent[filepath.ToSlash(v.File())]; ok {
			if shouldSuppressAfterFix(v, content, fileConfigs) {
				continue
			}
		}

		remaining = append(remaining, v)
	}

	return remaining
}

// shouldSuppressAfterFix checks whether a violation should be suppressed
// because a PostFixRevalidator determined the condition no longer holds
// after other fixes were applied.
func shouldSuppressAfterFix(
	v rules.Violation,
	modifiedContent []byte,
	fileConfigs map[string]*config.Config,
) bool {
	rule := rules.DefaultRegistry().Get(v.RuleCode)
	if rule == nil {
		return false
	}
	revalidator, ok := rule.(rules.PostFixRevalidator)
	if !ok {
		return false
	}

	var ruleCfg any
	filePath := filepath.ToSlash(v.File())
	fileCfg := fileConfigs[filePath]
	if fileCfg == nil {
		// Fallback: fileConfigs may use platform-specific paths (Windows).
		fileCfg = fileConfigs[v.File()]
	}
	if fileCfg != nil {
		ruleCfg = fileCfg.Rules.GetOptions(v.RuleCode)
	}

	return !revalidator.RevalidateAfterFix(v, modifiedContent, ruleCfg)
}
