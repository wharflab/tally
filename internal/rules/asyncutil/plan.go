// Package asyncutil provides shared helpers for async rule implementations.
package asyncutil

import (
	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// HandlerFactory creates a ResultHandler for a specific stage and platform.
type HandlerFactory func(
	meta rules.RuleMetadata,
	info *semantic.StageInfo,
	file, platform string,
) async.ResultHandler

// PlanExternalImageChecks builds async check requests for all external image
// stages using the shared iteration, platform resolution, and dedup-key logic.
// Each rule provides its own HandlerFactory to create the appropriate handler.
func PlanExternalImageChecks(
	input rules.LintInput,
	meta rules.RuleMetadata,
	fn HandlerFactory,
) []async.CheckRequest {
	sem := input.Semantic
	if sem == nil {
		return nil
	}

	requests := make([]async.CheckRequest, 0, sem.StageCount())
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
			Handler:    fn(meta, info, input.File, expectedPlatform),
		})
	}

	return requests
}

// RefinedViolations suppresses fast-path violations for every stage in
// stageIndices and re-emits refined violations produced by the refine callback.
// This is the shared "suppress + re-emit" loop used by async handlers that
// resolve base-image metadata from the registry.
func RefinedViolations(
	ruleCode, file string,
	stageIndices []int,
	hasViolation func(stageIdx int) bool,
	refine func(stageIdx int) []any,
) []any {
	var out []any
	for _, idx := range stageIndices {
		if !hasViolation(idx) {
			continue
		}
		out = append(out, async.CompletedCheck{
			RuleCode:   ruleCode,
			File:       file,
			StageIndex: idx,
		})
		out = append(out, refine(idx)...)
	}
	return out
}
