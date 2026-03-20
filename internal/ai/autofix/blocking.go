package autofix

import (
	"fmt"
	"slices"

	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/rules"
)

func collectBlockingIssues(violations []rules.Violation) []autofixdata.BlockingIssue {
	blocking := make([]autofixdata.BlockingIssue, 0, 8)
	seen := make(map[string]struct{})

	for _, v := range violations {
		isBlocking := v.Severity == rules.SeverityError || v.RuleCode == unreachableStagesKey
		if !isBlocking {
			continue
		}
		key := v.RuleCode + "|" + v.Message + "|" + v.Location.File + "|" +
			fmt.Sprintf("%d:%d-%d:%d", v.Location.Start.Line, v.Location.Start.Column, v.Location.End.Line, v.Location.End.Column)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		issue := autofixdata.BlockingIssue{
			Rule:    v.RuleCode,
			Message: v.Message,
		}
		if !v.Location.IsFileLevel() {
			issue.Line = v.Location.Start.Line
			issue.Column = v.Location.Start.Column
		}
		if v.SourceCode != "" {
			issue.Snippet = v.SourceCode
		}
		blocking = append(blocking, issue)
	}

	slices.SortFunc(blocking, func(a, b autofixdata.BlockingIssue) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		if a.Rule != b.Rule {
			if a.Rule < b.Rule {
				return -1
			}
			return 1
		}
		if a.Message < b.Message {
			return -1
		}
		if a.Message > b.Message {
			return 1
		}
		return 0
	})

	return blocking
}
