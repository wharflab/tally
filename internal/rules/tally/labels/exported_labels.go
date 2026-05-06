package labels

import (
	"slices"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
)

type labelPairID struct {
	stageIndex   int
	commandIndex int
	pairIndex    int
}

type labelCommandKey struct {
	stageIndex   int
	commandIndex int
	key          string
}

func exportedImageStageChain(input rules.LintInput) []int {
	finalStage := input.FinalStageIndex()
	if finalStage < 0 {
		return nil
	}

	chain := []int{finalStage}
	seen := map[int]bool{finalStage: true}
	for current := finalStage; input.Semantic != nil; {
		info := input.Semantic.StageInfo(current)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef {
			break
		}
		parent := info.BaseImage.StageIndex
		if parent < 0 || seen[parent] {
			break
		}
		seen[parent] = true
		chain = append(chain, parent)
		current = parent
	}
	slices.Reverse(chain)
	return chain
}

func activeExportedLabelPairIDs(input rules.LintInput) map[labelPairID]bool {
	active := activeExportedLabelPairsByKey(input)
	if len(active) == 0 {
		return nil
	}

	ids := make(map[labelPairID]bool, len(active))
	for _, pair := range active {
		ids[labelPairKey(pair)] = true
	}
	return ids
}

func activeExportedLabelCommandKeys(input rules.LintInput) map[labelCommandKey]bool {
	active := activeExportedLabelPairsByKey(input)
	if len(active) == 0 {
		return nil
	}

	keys := make(map[labelCommandKey]bool, len(active))
	for key, pair := range active {
		keys[labelCommandKey{
			stageIndex:   pair.StageIndex,
			commandIndex: pair.CommandIndex,
			key:          key,
		}] = true
	}
	return keys
}

func activeExportedLabelPairsByKey(input rules.LintInput) map[string]facts.LabelPairFact {
	if input.Facts == nil {
		return nil
	}

	chain := exportedImageStageChain(input)
	if len(chain) == 0 {
		return nil
	}

	stages := input.Facts.Stages()
	active := map[string]facts.LabelPairFact{}
	for _, stageIdx := range chain {
		if stageIdx < 0 || stageIdx >= len(stages) || stages[stageIdx] == nil {
			continue
		}
		for _, pair := range stages[stageIdx].Labels {
			if pair.KeyIsDynamic || pair.Key == "" {
				continue
			}
			active[pair.Key] = pair
		}
	}
	return active
}

func exportedLabelPairsByKey(input rules.LintInput, key string) []facts.LabelPairFact {
	if input.Facts == nil || key == "" {
		return nil
	}

	chain := exportedImageStageChain(input)
	if len(chain) == 0 {
		return nil
	}

	stages := input.Facts.Stages()
	var pairs []facts.LabelPairFact
	for _, stageIdx := range chain {
		if stageIdx < 0 || stageIdx >= len(stages) || stages[stageIdx] == nil {
			continue
		}
		for _, pair := range stages[stageIdx].Labels {
			if pair.KeyIsDynamic || pair.Key != key {
				continue
			}
			pairs = append(pairs, pair)
		}
	}
	return pairs
}

func labelPairKey(pair facts.LabelPairFact) labelPairID {
	return labelPairID{
		stageIndex:   pair.StageIndex,
		commandIndex: pair.CommandIndex,
		pairIndex:    pair.PairIndex,
	}
}
