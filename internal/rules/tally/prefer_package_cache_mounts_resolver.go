package tally

import (
	"bytes"
	"context"
	"slices"
	"strings"

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

	targetRunFacts := findRunFactsForResolveData(parseResult.Stages[data.StageIndex], stageFacts, data)
	if targetRunFacts == nil {
		return nil, nil
	}

	selectedEnvEntries := selectResolvedCacheEnvEntries(
		cacheEnvEntriesFromFacts(targetRunFacts.CacheDisablingEnv),
		data.CacheEnvSelections,
	)
	sm := sourcemap.New(resolveCtx.Content)
	return computeCacheMountEditsForRun(resolveCtx.FilePath, targetRunFacts, sm, selectedEnvEntries), nil
}

type resolvedRunCandidate struct {
	runFacts *facts.RunFacts
	ordinal  int
}

func findRunFactsForResolveData(
	stage instructions.Stage,
	stageFacts *facts.StageFacts,
	data *rules.CacheMountsResolveData,
) *facts.RunFacts {
	if data == nil {
		return nil
	}

	candidates := collectResolvedRunCandidates(stage, stageFacts)
	if len(candidates) == 0 {
		return nil
	}

	var (
		best      *facts.RunFacts
		bestScore = -1
	)
	for _, candidate := range candidates {
		analysis := analyzeRunForCacheMounts(candidate.runFacts)
		if analysis == nil {
			continue
		}
		if len(data.RequiredTargets) > 0 && !slices.Equal(requiredTargets(analysis.required), data.RequiredTargets) {
			continue
		}
		if len(data.CommandNames) > 0 && !slices.Equal(resolverCommandNames(candidate.runFacts.CommandInfos), data.CommandNames) {
			continue
		}
		score := resolvedRunMatchScore(candidate.runFacts, candidate.ordinal, data)
		if score < bestScore {
			continue
		}
		best = candidate.runFacts
		bestScore = score
	}

	if best != nil {
		return best
	}

	if data.RunOrdinal <= 0 || data.RunOrdinal > len(candidates) {
		return nil
	}
	analysis := analyzeRunForCacheMounts(candidates[data.RunOrdinal-1].runFacts)
	if analysis == nil {
		return nil
	}
	if len(data.RequiredTargets) > 0 && !slices.Equal(requiredTargets(analysis.required), data.RequiredTargets) {
		return nil
	}
	return candidates[data.RunOrdinal-1].runFacts
}

func collectResolvedRunCandidates(
	stage instructions.Stage,
	stageFacts *facts.StageFacts,
) []resolvedRunCandidate {
	if stageFacts == nil {
		return nil
	}

	runFactsByRun := make(map[*instructions.RunCommand]*facts.RunFacts, len(stageFacts.Runs))
	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil || runFacts.Run == nil {
			continue
		}
		runFactsByRun[runFacts.Run] = runFacts
	}

	candidates := make([]resolvedRunCandidate, 0, len(stageFacts.Runs))
	runOrdinal := 0
	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			continue
		}
		runOrdinal++
		runFacts := runFactsByRun[run]
		if runFacts == nil {
			continue
		}
		candidates = append(candidates, resolvedRunCandidate{
			runFacts: runFacts,
			ordinal:  runOrdinal,
		})
	}
	return candidates
}

func resolvedRunMatchScore(
	runFacts *facts.RunFacts,
	runOrdinal int,
	data *rules.CacheMountsResolveData,
) int {
	if runFacts == nil {
		return -1
	}

	score := 0
	signature := normalizeResolverRunSignature(runFacts.CommandScript)
	switch {
	case signature != "" && signature == data.RunSignature:
		score += 100
	case signature != "" && data.RunSignature != "" &&
		(strings.Contains(signature, data.RunSignature) || strings.Contains(data.RunSignature, signature)):
		score += 50
	default:
		score += sharedResolverSignatureTokens(signature, data.RunSignature)
	}

	if data.RunOrdinal > 0 {
		score -= abs(runOrdinal - data.RunOrdinal)
	}
	return score
}

func sharedResolverSignatureTokens(a, b string) int {
	if a == "" || b == "" {
		return 0
	}

	tokens := make(map[string]bool)
	for token := range strings.FieldsSeq(a) {
		tokens[token] = true
	}

	shared := 0
	seen := make(map[string]bool)
	for token := range strings.FieldsSeq(b) {
		if seen[token] || !tokens[token] {
			continue
		}
		seen[token] = true
		shared++
	}
	return shared
}

func selectResolvedCacheEnvEntries(
	entries []cacheEnvEntry,
	selections []rules.CacheMountsEnvSelection,
) []cacheEnvEntry {
	if len(selections) == 0 {
		return nil
	}

	selected := make([]cacheEnvEntry, 0, len(selections))
	for _, selection := range selections {
		if selection.BindingIndex < 0 || selection.BindingIndex >= len(entries) {
			continue
		}
		entry := entries[selection.BindingIndex]
		if entry.key != selection.Key {
			continue
		}
		selected = append(selected, entry)
	}
	return selected
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func init() {
	fix.RegisterResolver(&cacheMountsResolver{})
}
