package tally

import (
	"bytes"
	"context"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/sourcemap"
)

// cacheMountsResolver re-runs prefer-package-cache-mounts analysis on the
// post-sync-fix Dockerfile and emits the set of edits (mount insertion,
// tail rewrite, env removals) that would have been produced synchronously.
//
// Running post-sync lets narrow fixes (shellcheck SC2086 quoting,
// curl-should-follow-redirects flag insertion, ...) land first and be
// preserved when this rule needs to rewrite the full RUN tail, rather
// than having them conflict-evicted by the large replacement range.
type cacheMountsResolver struct{}

func (r *cacheMountsResolver) ID() string { return rules.CacheMountsResolverID }

func (r *cacheMountsResolver) Resolve(
	_ context.Context,
	resolveCtx fix.ResolveContext,
	f *rules.SuggestedFix,
) ([]rules.TextEdit, error) {
	data, ok := f.ResolverData.(*rules.CacheMountsResolveData)
	if !ok || data == nil {
		return nil, nil
	}

	parseResult, err := dockerfile.Parse(bytes.NewReader(resolveCtx.Content), nil)
	if err != nil {
		return nil, nil //nolint:nilerr // best-effort: skip silently on parse failure
	}
	if data.StageIndex < 0 || data.StageIndex >= len(parseResult.Stages) {
		return nil, nil
	}

	sem := semantic.NewBuilder(parseResult, nil, resolveCtx.FilePath).Build()
	fileFacts := facts.NewFileFacts(resolveCtx.FilePath, parseResult, sem, nil, nil)
	stageFacts := fileFacts.Stage(data.StageIndex)
	if stageFacts == nil {
		return nil, nil
	}

	targetRunFacts := findRunFactsByOrdinal(parseResult.Stages[data.StageIndex], stageFacts, data.RunOrdinal)
	if targetRunFacts == nil {
		return nil, nil
	}

	sm := sourcemap.New(resolveCtx.Content)
	return computeCacheMountEditsForRun(resolveCtx.FilePath, targetRunFacts, sm), nil
}

// findRunFactsByOrdinal returns the RunFacts for the 1-based shell-form RUN
// ordinal within stage, or nil when no match is found.
func findRunFactsByOrdinal(
	stage instructions.Stage,
	stageFacts *facts.StageFacts,
	ordinal int,
) *facts.RunFacts {
	if ordinal <= 0 {
		return nil
	}
	seen := 0
	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			continue
		}
		seen++
		if seen != ordinal {
			continue
		}
		for _, rf := range stageFacts.Runs {
			if rf != nil && rf.Run == run {
				return rf
			}
		}
		return nil
	}
	return nil
}

func init() {
	fix.RegisterResolver(&cacheMountsResolver{})
}
