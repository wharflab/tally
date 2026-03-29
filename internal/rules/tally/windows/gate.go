package windows

import (
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// windowsStages returns stages that target Windows based on the semantic model's
// BaseImageOS detection. This is the shared gate for all tally/windows/* rules.
func windowsStages(input rules.LintInput) []*semantic.StageInfo {
	sem := input.Semantic
	if sem == nil {
		return nil
	}

	stages := make([]*semantic.StageInfo, 0, sem.StageCount())
	for i := range sem.StageCount() {
		info := sem.StageInfo(i)
		if info == nil {
			continue
		}
		if info.IsWindows() {
			stages = append(stages, info)
		}
	}

	return stages
}
