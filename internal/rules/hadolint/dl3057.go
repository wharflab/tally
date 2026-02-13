package hadolint

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/async"
	"github.com/tinovyatkin/tally/internal/registry"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/asyncutil"
	"github.com/tinovyatkin/tally/internal/semantic"
)

// DL3057Rule implements the DL3057 linting rule.
//
// Fast path (static): If no stage in the Dockerfile contains a HEALTHCHECK CMD
// instruction, emits a single file-level violation (StageIndex = -1). This is
// conservative — it may be a false positive when a base image already defines
// HEALTHCHECK, since Docker inherits it at runtime.
//
// Async path (registry-backed): For each external base image, checks whether it
// defines a HEALTHCHECK. If so, emits CompletedCheck to suppress the fast-path
// violation. Additionally detects useless HEALTHCHECK NONE instructions when the
// base image has no healthcheck to disable.
type DL3057Rule struct{}

// NewDL3057Rule creates a new DL3057 rule instance.
func NewDL3057Rule() *DL3057Rule {
	return &DL3057Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3057Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3057",
		Name:            "HEALTHCHECK instruction missing",
		Description:     "`HEALTHCHECK` instruction missing",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3057",
		DefaultSeverity: rules.SeverityInfo,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// Check implements the fast path for DL3057.
//
// If any stage contains a HEALTHCHECK CMD (not NONE), no violation is reported.
// Otherwise, a single file-level violation with StageIndex=-1 is emitted. The
// async path may later suppress this if a base image provides HEALTHCHECK.
func (r *DL3057Rule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		return nil
	}

	if sem.StageCount() == 0 {
		return nil
	}

	// If any stage has an explicit HEALTHCHECK CMD, no violation needed.
	for i := range input.Stages {
		if stageHasHealthcheckCmd(&input.Stages[i]) {
			return nil
		}
	}

	// No HEALTHCHECK CMD anywhere — emit a file-level violation.
	meta := r.Metadata()
	loc := rules.NewFileLocation(input.File)
	v := rules.NewViolation(
		loc,
		meta.Code,
		meta.Description,
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"Add a HEALTHCHECK instruction to enable container health monitoring. " +
			"Use HEALTHCHECK CMD to define a check command, or HEALTHCHECK NONE " +
			"to explicitly opt out. Note: HEALTHCHECK is inherited from base images " +
			"at runtime, so this may be a false positive if your base image already " +
			"defines one.",
	)
	v.StageIndex = -1
	return []rules.Violation{v}
}

// PlanAsync creates check requests for each external base image to resolve
// whether it defines a HEALTHCHECK.
func (r *DL3057Rule) PlanAsync(input rules.LintInput) []async.CheckRequest {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	// If any stage already has HEALTHCHECK CMD, Check() returns nil so
	// async refinement is unnecessary.
	for i := range input.Stages {
		if stageHasHealthcheckCmd(&input.Stages[i]) {
			return nil
		}
	}

	meta := r.Metadata()
	return asyncutil.PlanExternalImageChecks(input, meta, func(
		m rules.RuleMetadata,
		info *semantic.StageInfo,
		file, _ string,
	) async.ResultHandler {
		return &healthcheckHandler{
			meta:     m,
			file:     file,
			stageIdx: info.Index,
			semantic: sem,
			stages:   input.Stages,
		}
	})
}

// healthcheckHandler processes resolved image config for HEALTHCHECK detection.
type healthcheckHandler struct {
	meta     rules.RuleMetadata
	file     string
	stageIdx int
	semantic *semantic.Model
	stages   []instructions.Stage
}

func (h *healthcheckHandler) OnSuccess(resolved any) []any {
	cfg, ok := resolved.(*registry.ImageConfig)
	if !ok || cfg == nil {
		return nil
	}

	out := make([]any, 0)

	if cfg.HasHealthcheck {
		// Base image has HEALTHCHECK — suppress the file-level fast violation.
		out = append(out, async.CompletedCheck{
			RuleCode:   h.meta.Code,
			File:       h.file,
			StageIndex: -1, // Matches the fast-path violation's StageIndex
		})
		return out
	}

	// Base image has no HEALTHCHECK. Check this stage and descendants for
	// useless HEALTHCHECK NONE instructions.
	descendants := findDescendants(h.semantic, h.stageIdx)
	allStages := append([]int{h.stageIdx}, descendants...)

	for _, idx := range allStages {
		if idx < 0 || idx >= len(h.stages) {
			continue
		}
		if loc := healthcheckNoneLocation(&h.stages[idx]); loc != nil {
			// HEALTHCHECK NONE with no inherited HC to disable — useless.
			v := rules.NewViolation(
				rules.NewLocationFromRanges(h.file, loc),
				h.meta.Code,
				"`HEALTHCHECK NONE` has no effect: base image has no health check to disable",
				h.meta.DefaultSeverity,
			).WithDocURL(h.meta.DocURL)
			v.StageIndex = idx

			// Suppress the generic "missing" violation since we have a specific one.
			out = append(out,
				async.CompletedCheck{
					RuleCode:   h.meta.Code,
					File:       h.file,
					StageIndex: -1,
				},
				v,
			)
		}
	}

	// Don't emit CompletedCheck(-1) when base has no HC and no HEALTHCHECK NONE:
	// the fast-path "missing" violation should remain.
	return out
}

// stageHasHealthcheckCmd checks whether a stage contains a HEALTHCHECK CMD instruction
// (not HEALTHCHECK NONE).
func stageHasHealthcheckCmd(stage *instructions.Stage) bool {
	for _, cmd := range stage.Commands {
		if hc, ok := cmd.(*instructions.HealthCheckCommand); ok {
			if hc.Health != nil && hc.Health.Test != nil &&
				len(hc.Health.Test) > 0 && hc.Health.Test[0] != "NONE" {
				return true
			}
		}
	}
	return false
}

// healthcheckNoneLocation returns the location of the first HEALTHCHECK NONE
// instruction in a stage, or nil if none exists.
func healthcheckNoneLocation(stage *instructions.Stage) []parser.Range {
	for _, cmd := range stage.Commands {
		if hc, ok := cmd.(*instructions.HealthCheckCommand); ok {
			if hc.Health != nil && hc.Health.Test != nil &&
				len(hc.Health.Test) > 0 && hc.Health.Test[0] == "NONE" {
				return hc.Location()
			}
		}
	}
	return nil
}

// findDescendants returns the indices of all stages that transitively inherit
// from the given stage via FROM chain.
func findDescendants(sem *semantic.Model, stageIdx int) []int {
	var result []int
	stageCount := sem.StageCount()
	for i := range stageCount {
		info := sem.StageInfo(i)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef {
			continue
		}
		if info.BaseImage.StageIndex == stageIdx {
			result = append(result, i)
			// Recurse to find grandchildren.
			result = append(result, findDescendants(sem, i)...)
		}
	}
	return result
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3057Rule())
}
