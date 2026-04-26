package linter

import (
	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/rules"
)

// MergeAsyncViolations replaces fast-path violations for completed async checks
// with the async results for the same (rule, file, stage) tuple.
func MergeAsyncViolations(fast []rules.Violation, asyncResult *async.RunResult) []rules.Violation {
	if asyncResult == nil {
		return fast
	}

	var asyncViolations []rules.Violation
	for _, v := range asyncResult.Violations {
		if viol, ok := v.(rules.Violation); ok {
			asyncViolations = append(asyncViolations, viol)
		}
	}

	if len(asyncResult.Completed) == 0 {
		if len(asyncViolations) > 0 {
			return append(fast, asyncViolations...)
		}
		return fast
	}

	type ruleFileStage struct {
		ruleCode      string
		invocationKey string
		file          string
		stageIndex    int
	}

	completedSet := make(map[ruleFileStage]bool, len(asyncResult.Completed))
	for _, c := range asyncResult.Completed {
		completedSet[ruleFileStage{
			ruleCode:      c.RuleCode,
			invocationKey: c.InvocationKey,
			file:          c.File,
			stageIndex:    c.StageIndex,
		}] = true
	}

	merged := make([]rules.Violation, 0, len(fast)+len(asyncViolations))
	for _, v := range fast {
		if completedSet[ruleFileStage{
			ruleCode:      v.RuleCode,
			invocationKey: v.InvocationKey,
			file:          v.File(),
			stageIndex:    v.StageIndex,
		}] {
			continue
		}
		merged = append(merged, v)
	}

	return append(merged, asyncViolations...)
}
