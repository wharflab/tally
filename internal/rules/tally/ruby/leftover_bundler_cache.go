package ruby

import (
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	rubyfacts "github.com/wharflab/tally/internal/facts/ruby"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// LeftoverBundlerCacheRuleCode is the full rule code.
const LeftoverBundlerCacheRuleCode = rules.TallyRulePrefix + "ruby/leftover-bundler-cache"

// leftoverBundlerCacheFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const leftoverBundlerCacheFixPriority = 88

// LeftoverBundlerCacheRule flags stages that run `bundle install` without
// the canonical Rails-generator cleanup of bundler-cache directories.
type LeftoverBundlerCacheRule struct{}

// NewLeftoverBundlerCacheRule creates the rule.
func NewLeftoverBundlerCacheRule() *LeftoverBundlerCacheRule {
	return &LeftoverBundlerCacheRule{}
}

// Metadata returns the rule metadata.
func (r *LeftoverBundlerCacheRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            LeftoverBundlerCacheRuleCode,
		Name:            "Bundler leaves cache directories behind after install",
		Description:     "`bundle install` leaves cache directories behind that bloat the final image",
		DocURL:          rules.TallyDocURL(LeftoverBundlerCacheRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		FixPriority:     leftoverBundlerCacheFixPriority,
	}
}

// largeLockfileGemCount is the gem-count threshold above which severity
// bumps from info to warning. The corpus shows that projects with ≥ 80
// gems pay a much steeper image-size cost when the cleanup is missed.
const largeLockfileGemCount = 80

// Check runs the rule.
func (r *LeftoverBundlerCacheRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()
	rubyFacts := input.Facts.RubyFacts()

	var violations []rules.Violation
	forEachRubyStage(input, func(stageIdx int, _ instructions.Stage, sf *facts.StageFacts) {
		// Skip stages whose only purpose is to build gems for COPY-out
		// (the cache only matters in stages whose layers ship in the
		// final image).
		if stageIsBuilderForCopyOut(input.Semantic, stageIdx, len(input.Stages)) {
			return
		}
		violations = append(violations, r.checkStage(input, sf, sm, rubyFacts, meta)...)
	})
	return violations
}

func (r *LeftoverBundlerCacheRule) checkStage(
	input rules.LintInput,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	rubyFacts *rubyfacts.RubyFacts,
	meta rules.RuleMetadata,
) []rules.Violation {
	// Find the *last* `bundle install` in the stage; cleanup must happen
	// after it to be effective. If a later RUN already runs cleanup, the
	// rule is satisfied.
	var lastInstall *facts.RunFacts
	var lastInstallCmd shell.CommandInfo
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		install := findFirstBundleInstall(runFacts)
		if install != nil {
			lastInstall = runFacts
			lastInstallCmd = *install
		}
	}
	if lastInstall == nil {
		return nil
	}

	// Did any RUN at-or-after the last install perform cleanup?
	cleanupAfterIdx := -1
	for i, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		if i < indexOfRunFacts(sf.Runs, lastInstall) {
			continue
		}
		if runHasBundlerCacheCleanup(runFacts) {
			cleanupAfterIdx = i
			break
		}
		if runRunsBundleClean(runFacts) {
			cleanupAfterIdx = i
			break
		}
	}
	if cleanupAfterIdx >= 0 {
		return nil
	}

	severity := meta.DefaultSeverity
	if rubyFacts != nil && rubyFacts.Lockfile != nil &&
		len(rubyFacts.Lockfile.Specs) >= largeLockfileGemCount {
		// Many gems → cleanup matters more. Bump severity.
		severity = rules.SeverityWarning
	}

	loc := bundleInstallViolationLocation(input.File, lastInstall, lastInstallCmd, sm)
	v := rules.NewViolation(loc, meta.Code, meta.Description, severity).
		WithDocURL(meta.DocURL).
		WithDetail(leftoverBundlerCacheDetail(rubyFacts))
	if fix := buildLeftoverBundlerCacheFix(input, sf, lastInstall, sm, meta.FixPriority); fix != nil {
		v = v.WithSuggestedFix(fix)
	}
	return []rules.Violation{v}
}

func leftoverBundlerCacheDetail(rubyFacts *rubyfacts.RubyFacts) string {
	base := "After `bundle install` runs, Bundler leaves three cache directories behind that ship in the " +
		"final image layer: `~/.bundle/`, `${BUNDLE_PATH}/ruby/*/cache` (gem `.gem` archives), and " +
		"`${BUNDLE_PATH}/ruby/*/bundler/gems/*/.git` (full git history of git-sourced gems). The Rails " +
		"generator template explicitly removes them after install. For non-trivial Rails apps these can " +
		"cost 50–200 MiB of final-image weight."
	if rubyFacts != nil && rubyFacts.Lockfile != nil && len(rubyFacts.Lockfile.Specs) > 0 {
		base += " (This project resolves " + strconv.Itoa(len(rubyFacts.Lockfile.Specs)) +
			" gems in `Gemfile.lock`, so the impact is on the higher end.)"
	}
	return base
}

// stageIsBuilderForCopyOut reports whether the stage is referenced by a
// later stage's `COPY --from=`. Such stages are throwaway — their layers
// don't ship in the final image, so cache bloat doesn't matter.
func stageIsBuilderForCopyOut(sem *semantic.Model, stageIdx, stageCount int) bool {
	if sem == nil {
		return false
	}
	for i := stageIdx + 1; i < stageCount; i++ {
		other := sem.StageInfo(i)
		if other == nil {
			continue
		}
		for _, ref := range other.CopyFromRefs {
			if ref.IsStageRef && ref.StageIndex == stageIdx {
				return true
			}
		}
	}
	return false
}

// runHasBundlerCacheCleanup reports whether the RUN deletes (or otherwise
// invalidates) at least one of the three canonical Bundler cache
// directories the Rails generator strips after install.
//
// Detection is substring-based on the source script — the paths involve
// shell globs (`*`) and parameter expansion (`${BUNDLE_PATH}`) that the
// CommandInfo args layer doesn't always preserve verbatim, so a fast
// substring check is more robust than walking parsed command args.
func runHasBundlerCacheCleanup(runFacts *facts.RunFacts) bool {
	if runFacts == nil {
		return false
	}
	script := runFacts.SourceScript
	if script == "" {
		return false
	}
	// The cleanup typically uses one of these path fragments. Match any.
	signals := []string{
		"~/.bundle",
		"$HOME/.bundle",
		"${HOME}/.bundle",
		"/.bundle/",
		"BUNDLE_PATH/cache",
		"BUNDLE_PATH}/cache",
		"/cache",
		"bundler/gems/*/.git",
		"/bundler/gems",
	}
	// Require an `rm` invocation alongside one of the path fragments so
	// that a stray reference doesn't trip a false positive.
	if !runScriptInvokesCleanupCommand(script) {
		return false
	}
	for _, s := range signals {
		if strings.Contains(script, s) {
			return true
		}
	}
	return false
}

// runRunsBundleClean reports whether the RUN runs `bundle clean --force`,
// which Bundler's own cleanup mechanism produces an equivalent result for
// the cache/ directory.
func runRunsBundleClean(runFacts *facts.RunFacts) bool {
	if runFacts == nil {
		return false
	}
	for _, ci := range runFacts.CommandInfos {
		if !strings.EqualFold(ci.Name, "bundle") {
			continue
		}
		if !strings.EqualFold(ci.Subcommand, "clean") {
			continue
		}
		// `bundle clean --force` (or `--force --dry-run` etc.) — require
		// `--force` because that's what actually deletes content.
		for _, a := range ci.Args {
			if a == "--force" || a == "-f" {
				return true
			}
		}
	}
	return false
}

// runScriptInvokesCleanupCommand reports whether a script contains an `rm`
// invocation. Used to gate substring-based path detection on actual
// removal commands.
func runScriptInvokesCleanupCommand(script string) bool {
	// Word-boundary check: `rm ` or `rm\n` or `rm\t`, with leading-space
	// tolerance for chained commands.
	idx := 0
	for idx < len(script) {
		next := strings.Index(script[idx:], "rm")
		if next < 0 {
			return false
		}
		pos := idx + next
		// Must be at start, after whitespace, or after a separator.
		if pos > 0 {
			prev := script[pos-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != ';' && prev != '&' && prev != '|' {
				idx = pos + 2
				continue
			}
		}
		// Followed by whitespace or a flag (- prefix).
		if pos+2 >= len(script) {
			return false
		}
		nextCh := script[pos+2]
		if nextCh == ' ' || nextCh == '\t' || nextCh == '\n' || nextCh == '-' {
			return true
		}
		idx = pos + 2
	}
	return false
}

// indexOfRunFacts returns the slice index of target in runs, or -1.
func indexOfRunFacts(runs []*facts.RunFacts, target *facts.RunFacts) int {
	for i, r := range runs {
		if r == target {
			return i
		}
	}
	return -1
}

// buildLeftoverBundlerCacheFix appends the canonical Rails-generator-style
// cleanup to the `bundle install` step. The fix is a FixSuggestion because
// the exact path varies with Bundler version / BUNDLE_PATH setting, and
// the user may have configured a non-default location.
func buildLeftoverBundlerCacheFix(
	input rules.LintInput,
	sf *facts.StageFacts,
	installRun *facts.RunFacts,
	sm *sourcemap.SourceMap,
	priority int,
) *rules.SuggestedFix {
	if sf == nil || sm == nil || installRun == nil || installRun.Run == nil {
		return nil
	}
	runRanges := installRun.Run.Location()
	if len(runRanges) == 0 {
		return nil
	}
	endLine := sm.ResolveEndLine(runRanges[len(runRanges)-1].End.Line)
	if endLine <= 0 {
		return nil
	}
	insertLine := endLine + 1
	cleanup := "RUN rm -rf ~/.bundle/ \"${BUNDLE_PATH}\"/ruby/*/cache \"${BUNDLE_PATH}\"/ruby/*/bundler/gems/*/.git\n"
	return &rules.SuggestedFix{
		Description: "Strip Bundler's cache directories after install (matches the Rails generator template)",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(input.File, insertLine, 0, insertLine, 0),
			NewText:  cleanup,
		}},
	}
}

func init() {
	rules.Register(NewLeftoverBundlerCacheRule())
}
