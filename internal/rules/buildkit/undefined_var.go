package buildkit

import (
	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// UndefinedVarRule implements BuildKit's UndefinedVar check.
//
// BuildKit runs this during LLB conversion by observing word expansion against
// the effective build environment. tally reimplements it as a static rule using
// the semantic model's stage-aware environment tracking.
type UndefinedVarRule struct{}

func NewUndefinedVarRule() *UndefinedVarRule {
	return &UndefinedVarRule{}
}

func (r *UndefinedVarRule) Metadata() rules.RuleMetadata {
	const name = "UndefinedVar"
	return *GetMetadata(name)
}

func (r *UndefinedVarRule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	meta := r.Metadata()
	var out []rules.Violation

	for stageIdx := range input.Stages {
		info := sem.StageInfo(stageIdx)
		if info == nil {
			continue
		}

		for _, undef := range info.UndefinedVars {
			loc := rules.NewLocationFromRanges(input.File, undef.Location)
			msg := linter.RuleUndefinedVar.Format(undef.Name, undef.Suggest)
			out = append(out, rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).WithDocURL(meta.DocURL))
		}
	}

	return out
}

func init() {
	rules.Register(NewUndefinedVarRule())
}
