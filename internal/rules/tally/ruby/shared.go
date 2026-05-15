package ruby

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/stagename"
)

// rubyStageVisitor is invoked once per stage that passes the common
// Ruby-rule prefilter (non-dev name, has StageFacts, non-Windows base,
// and matches stageLooksLikeRuby).
type rubyStageVisitor func(stageIdx int, stage instructions.Stage, sf *facts.StageFacts)

// forEachRubyStage walks input.Stages, applies the common Ruby-rule
// prefilter, and invokes visit for each stage that passes. The
// prefilter is the set of guards every Ruby rule shares:
//
//   - Skip stages whose name looks like dev/test/ci/debug.
//   - Skip stages with no StageFacts (Facts not populated for that stage).
//   - Skip stages on a Windows base image.
//   - Skip stages that don't look Ruby-shaped (so a Node stage in a
//     mixed-language Dockerfile doesn't trip a tally/ruby/* rule).
//
// Per-rule extra guards (production-stage detection, builder-for-copy-out
// detection, BuildKit-pragma gating, etc.) stay inside the visitor.
func forEachRubyStage(input rules.LintInput, visit rubyStageVisitor) {
	if input.Facts == nil {
		return
	}
	for stageIdx, stage := range input.Stages {
		if stagename.LooksLikeDev(stage.Name) {
			continue
		}
		sf := input.Facts.Stage(stageIdx)
		if sf == nil {
			continue
		}
		if sf.BaseImageOS == semantic.BaseImageOSWindows {
			continue
		}
		if !stageLooksLikeRuby(input.Semantic, stageIdx, stage, sf) {
			continue
		}
		visit(stageIdx, stage, sf)
	}
}

// buildStageTopEnvFix builds a SuggestedFix that inserts a single ENV
// line at the top of stage `sf` (the line immediately after `FROM`).
// This is the canonical placement used by the Rails 7.1 generator
// template for `ENV BUNDLE_*` declarations.
//
// Returns nil when the stage's location data is missing or the stage
// index is out of range.
func buildStageTopEnvFix(
	input rules.LintInput,
	sf *facts.StageFacts,
	priority int,
	envLine string,
	description string,
	isPreferred bool,
) *rules.SuggestedFix {
	if sf == nil {
		return nil
	}
	if sf.Index < 0 || sf.Index >= len(input.Stages) {
		return nil
	}
	stage := input.Stages[sf.Index]
	if len(stage.Location) == 0 {
		return nil
	}
	insertLine := stage.Location[len(stage.Location)-1].End.Line + 1
	return &rules.SuggestedFix{
		Description: description,
		Safety:      rules.FixSafe,
		Priority:    priority,
		IsPreferred: isPreferred,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(input.File, insertLine, 0, insertLine, 0),
			NewText:  envLine + "\n",
		}},
	}
}
