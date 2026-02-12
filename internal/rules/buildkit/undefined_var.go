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
			out = append(out, rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).WithDocURL(meta.DocURL))
		}
	}

	return out
}

// PlanAsync creates check requests to resolve base image env for external images.
// When resolved, the handler re-runs undefined-var analysis with actual env instead
// of the static approximation.
func (r *UndefinedVarRule) PlanAsync(input rules.LintInput) []async.CheckRequest {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	meta := r.Metadata()
	var requests []async.CheckRequest

	for info := range sem.ExternalImageStages() {
		if info.Stage == nil {
			continue
		}

		expectedPlatform, unresolved := semantic.ExpectedPlatform(info, sem)
		if len(unresolved) > 0 || expectedPlatform == "" {
			continue // skip when platform has unresolved ARGs or is empty
		}

		ref := info.Stage.BaseName
		key := ref + "|" + expectedPlatform

		requests = append(requests, async.CheckRequest{
			RuleCode:   meta.Code,
			Category:   async.CategoryNetwork,
			Key:        key,
			ResolverID: registry.RegistryResolverID(),
			Data:       &registry.ResolveRequest{Ref: ref, Platform: expectedPlatform},
			File:       input.File,
			StageIndex: info.Index,
			Handler: &undefinedVarHandler{
				meta:     meta,
				file:     input.File,
				stageIdx: info.Index,
				semantic: sem,
			},
		})
	}

	return requests
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

	var out []any
	for _, sr := range stageResults {
		for _, undef := range sr.Undefs {
			loc := rules.NewLocationFromRanges(h.file, undef.Location)
			msg := linter.RuleUndefinedVar.Format(undef.Name, undef.Suggest)
			out = append(out, rules.NewViolation(loc, h.meta.Code, msg, h.meta.DefaultSeverity).WithDocURL(h.meta.DocURL))
		}
	}

	return out
}

func init() {
	rules.Register(NewUndefinedVarRule())
}
