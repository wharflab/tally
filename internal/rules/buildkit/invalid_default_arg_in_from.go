package buildkit

import (
	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// InvalidDefaultArgInFromRule implements BuildKit's InvalidDefaultArgInFrom check.
//
// BuildKit runs this during LLB conversion by expanding base images using only
// default values for global ARGs. tally reimplements it as a static rule using
// the semantic model's FROM ARG analysis.
type InvalidDefaultArgInFromRule struct{}

func NewInvalidDefaultArgInFromRule() *InvalidDefaultArgInFromRule {
	return &InvalidDefaultArgInFromRule{}
}

func (r *InvalidDefaultArgInFromRule) Metadata() rules.RuleMetadata {
	const name = "InvalidDefaultArgInFrom"
	return *GetMetadata(name)
}

func (r *InvalidDefaultArgInFromRule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	meta := r.Metadata()
	var out []rules.Violation

	for stageIdx, stage := range input.Stages {
		info := sem.StageInfo(stageIdx)
		if info == nil || !info.FromArgs.InvalidDefaultBaseName {
			continue
		}

		loc := rules.NewLocationFromRanges(input.File, stage.Location)
		msg := linter.RuleInvalidDefaultArgInFrom.Format(stage.BaseName)
		out = append(out, rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).WithDocURL(meta.DocURL))
	}

	return out
}

func init() {
	rules.Register(NewInvalidDefaultArgInFromRule())
}
