package tally

import "github.com/wharflab/tally/internal/semantic"

func firstParentStageRefValue(
	sem *semantic.Model,
	stageIdx int,
	lookup func(parentIdx int) (string, bool),
) string {
	visited := make(map[int]bool)

	for idx := stageIdx; !visited[idx]; {
		visited[idx] = true

		info := sem.StageInfo(idx)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
			return ""
		}

		parentIdx := info.BaseImage.StageIndex
		if value, ok := lookup(parentIdx); ok {
			return value
		}

		idx = parentIdx
	}

	return ""
}
