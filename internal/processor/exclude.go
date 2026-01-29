package processor

import (
	"github.com/bmatcuk/doublestar/v4"

	"github.com/tinovyatkin/tally/internal/rules"
)

// PathExclusionFilter removes violations based on per-rule path exclusions.
// Uses config.Rules.GetExcludePaths() to get exclusion patterns for each rule.
type PathExclusionFilter struct{}

// NewPathExclusionFilter creates a new path exclusion filter processor.
func NewPathExclusionFilter() *PathExclusionFilter {
	return &PathExclusionFilter{}
}

// Name returns the processor's identifier.
func (p *PathExclusionFilter) Name() string {
	return "path-exclusion-filter"
}

// Process filters out violations for files that match exclusion patterns.
func (p *PathExclusionFilter) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
	return filterViolations(violations, func(v rules.Violation) bool {
		patterns := ctx.Config.Rules.GetExcludePaths(v.RuleCode)
		if len(patterns) == 0 {
			return true
		}

		for _, pattern := range patterns {
			matched, err := doublestar.Match(pattern, v.Location.File)
			if err != nil {
				// Invalid pattern - skip this check
				continue
			}
			if matched {
				return false // excluded
			}
		}

		return true
	})
}
