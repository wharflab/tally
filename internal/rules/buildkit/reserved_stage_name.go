package buildkit

import (
	"fmt"

	"github.com/tinovyatkin/tally/internal/rules"
)

// reservedStageNames matches BuildKit's set exactly (case-sensitive).
// BuildKit source: dockerfile2llb/convert.go
//
// Cross-rule interaction: StageNameCasing may suggest lowercasing stage names
// like "Scratch" to "scratch", which would then be flagged by this rule.
// This is the correct behavior — users should rename to a non-reserved name.
var reservedStageNames = map[string]struct{}{
	"context": {},
	"scratch": {},
}

// ReservedStageNameRule implements BuildKit's ReservedStageName check.
//
// BuildKit normally runs this during LLB conversion. tally reimplements it as a
// static rule based on the parsed stage list.
type ReservedStageNameRule struct{}

func NewReservedStageNameRule() *ReservedStageNameRule {
	return &ReservedStageNameRule{}
}

func (r *ReservedStageNameRule) Metadata() rules.RuleMetadata {
	const name = "ReservedStageName"
	return *GetMetadata(name)
}

func (r *ReservedStageNameRule) Check(input rules.LintInput) []rules.Violation {
	var out []rules.Violation
	meta := r.Metadata()

	for _, stage := range input.Stages {
		if stage.Name == "" {
			continue
		}
		if _, ok := reservedStageNames[stage.Name]; ok {
			loc := rules.NewLocationFromRanges(input.File, stage.Location)
			out = append(out, rules.NewViolation(
				loc,
				meta.Code,
				fmt.Sprintf("Stage name should not use the same name as reserved stage %q", stage.Name),
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL))
		}
	}

	return out
}

func init() {
	rules.Register(NewReservedStageNameRule())
}
