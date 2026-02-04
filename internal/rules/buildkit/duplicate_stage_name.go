package buildkit

import (
	"fmt"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
)

// DuplicateStageNameRule implements BuildKit's DuplicateStageName check.
//
// BuildKit normally runs this during LLB conversion. tally reimplements it as a
// static rule based on the parsed stage list.
type DuplicateStageNameRule struct{}

func NewDuplicateStageNameRule() *DuplicateStageNameRule {
	return &DuplicateStageNameRule{}
}

func (r *DuplicateStageNameRule) Metadata() rules.RuleMetadata {
	// Keep metadata aligned with internal BuildKit registry for docs.
	const name = "DuplicateStageName"
	return *GetMetadata(name)
}

func (r *DuplicateStageNameRule) Check(input rules.LintInput) []rules.Violation {
	seen := make(map[string]int)
	var out []rules.Violation

	for idx, stage := range input.Stages {
		if stage.Name == "" {
			continue
		}
		normalized := strings.ToLower(stage.Name)
		if existingIdx, exists := seen[normalized]; exists {
			loc := rules.NewLocationFromRanges(input.File, stage.Location)
			out = append(out, rules.NewViolation(
				loc,
				r.Metadata().Code,
				fmt.Sprintf("Stage name %q is already used on stage %d", stage.Name, existingIdx),
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL))
			continue
		}
		seen[normalized] = idx
	}

	return out
}

func init() {
	rules.Register(NewDuplicateStageNameRule())
}
