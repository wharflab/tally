package buildkit

import (
	"github.com/moby/buildkit/frontend/dockerfile/linter"

	"github.com/tinovyatkin/tally/internal/async"
	"github.com/tinovyatkin/tally/internal/registry"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// UndefinedVarRule implements BuildKit's UndefinedVar check.
//
// BuildKit runs this during LLB conversion by observing word expansion against
// the effective build environment. tally reimplements it as a static rule using
// the semantic model's stage-aware environment tracking.
//
// When async checks are enabled, PlanAsync resolves base image env from the
// registry, replacing the static approximation with actual values.
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
			v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).WithDocURL(meta.DocURL)
			v.StageIndex = stageIdx
			out = append(out, v)
		}
	}

	return out
}

// PlanAsync creates check requests to resolve base image env for external images.
// When resolved, the handler re-runs undefined-var analysis with actual env instead
// of the static approximation.
func (r *UndefinedVarRule) PlanAsync(input rules.LintInput) []async.CheckRequest {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		return nil
	}
	return planExternalImageChecks(input, r.Metadata(),
		func(meta rules.RuleMetadata, info *semantic.StageInfo, file, _ string) async.ResultHandler {
			return &undefinedVarHandler{
				meta:     meta,
				file:     file,
				stageIdx: info.Index,
				semantic: sem,
			}
		},
	)
}

// undefinedVarHandler re-runs undefined-var analysis using resolved base image env.
type undefinedVarHandler struct {
	meta     rules.RuleMetadata
	file     string
	stageIdx int
	semantic *semantic.Model
}

func (h *undefinedVarHandler) OnSuccess(resolved any) []any {
	cfg, ok := resolved.(*registry.ImageConfig)
	if !ok || cfg == nil {
		return nil
	}

	// Re-run undefined-var analysis for this stage and all stages that
	// transitively inherit from it, using actual base image env.
	stageResults := h.semantic.RecheckUndefinedVars(h.stageIdx, cfg.Env)

	// Non-nil slice signals "completed" to the runtime (even if empty).
	out := make([]any, 0)
	for _, sr := range stageResults {
		// Emit a CompletedCheck for each rechecked stage so the merge logic
		// knows to replace fast violations for descendant stages, not just
		// the directly resolved stage.
		if sr.StageIdx != h.stageIdx {
			out = append(out, async.CompletedCheck{
				RuleCode:   h.meta.Code,
				File:       h.file,
				StageIndex: sr.StageIdx,
			})
		}
		for _, undef := range sr.Undefs {
			loc := rules.NewLocationFromRanges(h.file, undef.Location)
			msg := linter.RuleUndefinedVar.Format(undef.Name, undef.Suggest)
			v := rules.NewViolation(loc, h.meta.Code, msg, h.meta.DefaultSeverity).WithDocURL(h.meta.DocURL)
			v.StageIndex = sr.StageIdx
			out = append(out, v)
		}
	}

	return out
}

func init() {
	rules.Register(NewUndefinedVarRule())
}
