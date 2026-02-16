package buildkit

import (
	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// UndefinedArgInFromRule implements BuildKit's UndefinedArgInFrom check.
//
// BuildKit normally runs this during LLB conversion. tally reimplements it as a
// static rule using the semantic model's FROM ARG analysis.
type UndefinedArgInFromRule struct{}

func NewUndefinedArgInFromRule() *UndefinedArgInFromRule {
	return &UndefinedArgInFromRule{}
}

func (r *UndefinedArgInFromRule) Metadata() rules.RuleMetadata {
	const name = "UndefinedArgInFrom"
	return *GetMetadata(name)
}

func (r *UndefinedArgInFromRule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	meta := r.Metadata()
	var out []rules.Violation

	for stageIdx, stage := range input.Stages {
		info := sem.StageInfo(stageIdx)
		if info == nil {
			continue
		}

		loc := rules.NewLocationFromRanges(input.File, stage.Location)

		// Match BuildKit behavior: report base name ARGs first, then platform ARGs.
		for _, undef := range info.FromArgs.UndefinedBaseName {
			msg := linter.RuleUndefinedArgInFrom.Format(undef.Name, undef.Suggest)
			out = append(out, rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).WithDocURL(meta.DocURL))
		}
		for _, undef := range info.FromArgs.UndefinedPlatform {
			msg := linter.RuleUndefinedArgInFrom.Format(undef.Name, undef.Suggest)
			out = append(out, rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).WithDocURL(meta.DocURL))
		}
	}

	return out
}

func init() {
	rules.Register(NewUndefinedArgInFromRule())
}
