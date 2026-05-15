package ruby

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/stagename"
)

// PreferNetworkNoneInstallRuleCode is the full rule code.
const PreferNetworkNoneInstallRuleCode = rules.TallyRulePrefix + "ruby/prefer-network-none-install"

// preferNetworkNoneInstallFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const preferNetworkNoneInstallFixPriority = 88

// PreferNetworkNoneInstallRule flags `bundle install` invocations on
// modern BuildKit Dockerfiles that don't use `RUN --network=none` for
// the install phase. The pattern is:
//
//  1. RUN bundle cache --no-install --all-platforms  (network required)
//  2. RUN --network=none bundle install --local       (network disabled)
//
// This is an advisory rule that surfaces the pattern at moments when
// the user is already on BuildKit syntax. It never fires unless the
// pattern would actually work.
type PreferNetworkNoneInstallRule struct{}

// NewPreferNetworkNoneInstallRule creates the rule.
func NewPreferNetworkNoneInstallRule() *PreferNetworkNoneInstallRule {
	return &PreferNetworkNoneInstallRule{}
}

// Metadata returns the rule metadata.
func (r *PreferNetworkNoneInstallRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferNetworkNoneInstallRuleCode,
		Name:            "Consider `bundle cache` + `RUN --network=none bundle install --local`",
		Description:     "BuildKit `RUN --network=none` enables a strictly reproducible offline install phase",
		DocURL:          rules.TallyDocURL(PreferNetworkNoneInstallRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "security",
		FixPriority:     preferNetworkNoneInstallFixPriority,
	}
}

// Check runs the rule.
func (r *PreferNetworkNoneInstallRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	// `--network=none` is BuildKit-only. Suppress when there's no
	// syntax pragma.
	if !hasBuildKitSyntaxPragma(input) {
		return nil
	}

	var violations []rules.Violation
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

		// Find a `bundle install` RUN that doesn't already use both
		// the bind-mount + cache-mount pattern. The advisory only
		// fires when the user is already on BuildKit AND is using
		// the modern manifest pattern — surfacing the further upgrade.
		runFacts := findBundleInstallRun(sf)
		if runFacts == nil {
			continue
		}
		if !runUsesBundleManifestBindAndCache(runFacts) {
			// The user hasn't even adopted the bind-mount + cache-mount
			// pattern yet. The prefer-gemfile-bind-mounts and
			// prefer-bundler-cache-mount rules cover that step.
			// Don't pile on with a second educational suggestion.
			continue
		}
		if runHasNetworkNoneFlag(runFacts) {
			continue
		}

		loc := bundleInstallViolationLocation(input.File, runFacts, *findFirstBundleInstall(runFacts), input.SourceMap())
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(preferNetworkNoneInstallDetail()).
			WithSuggestedFix(&rules.SuggestedFix{
				Description: "Split bundle install into two phases: " +
					"`RUN bundle cache --no-install --all-platforms` (network required), " +
					"then `RUN --network=none bundle install --local` (network disabled, " +
					"strictly reproducible)",
				Safety:      rules.FixSuggestion,
				Priority:    meta.FixPriority,
				IsPreferred: false,
			})
		violations = append(violations, v)
	}
	return violations
}

func preferNetworkNoneInstallDetail() string {
	return "Once `bundle install` runs with `--mount=type=bind` for the manifests and `--mount=type=cache` " +
		"for the gem cache, the next-best step is to split the install into two RUNs: `bundle cache " +
		"--no-install --all-platforms` (network required) and `RUN --network=none bundle install --local` " +
		"(network disabled). The result is a strictly reproducible install step and a defense-in-depth " +
		"boundary against malicious gems exfiltrating data at build time. Corpus uptake: 0/196 — " +
		"this is a niche but well-documented BuildKit feature worth surfacing."
}

// findBundleInstallRun returns the first RUN in the stage that runs
// `bundle install`, or nil when none exists.
func findBundleInstallRun(sf *facts.StageFacts) *facts.RunFacts {
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		if findFirstBundleInstall(runFacts) != nil {
			return runFacts
		}
	}
	return nil
}

// runUsesBundleManifestBindAndCache reports whether a RUN already uses
// both `--mount=type=bind` for Gemfile/Gemfile.lock AND
// `--mount=type=cache` for the Bundler cache target.
func runUsesBundleManifestBindAndCache(runFacts *facts.RunFacts) bool {
	if runFacts == nil || runFacts.Run == nil {
		return false
	}
	hasBindGemfile := false
	hasCache := false
	for _, mount := range runmount.GetMounts(runFacts.Run) {
		if mount == nil {
			continue
		}
		switch mount.Type {
		case instructions.MountTypeBind:
			if mount.Source == "Gemfile" || mount.Source == "Gemfile.lock" {
				hasBindGemfile = true
			}
		case instructions.MountTypeCache:
			if cacheTargetMatchesBundlerCache(mount.Target) {
				hasCache = true
			}
		case instructions.MountTypeTmpfs,
			instructions.MountTypeSecret,
			instructions.MountTypeSSH:
			// Other mount types are unrelated to this rule's
			// bind+cache check. Ignore.
		}
	}
	return hasBindGemfile && hasCache
}

// runHasNetworkNoneFlag reports whether the RUN carries
// `--network=none`.
func runHasNetworkNoneFlag(runFacts *facts.RunFacts) bool {
	if runFacts == nil || runFacts.Run == nil {
		return false
	}
	return instructions.GetNetwork(runFacts.Run) == instructions.NetworkNone
}

func init() {
	rules.Register(NewPreferNetworkNoneInstallRule())
}
