package ruby

import (
	"slices"
	"strings"

	rubyfacts "github.com/wharflab/tally/internal/facts/ruby"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/runmount"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/stagename"
)

// PreferBundlerCacheMountRuleCode is the full rule code.
const PreferBundlerCacheMountRuleCode = rules.TallyRulePrefix + "ruby/prefer-bundler-cache-mount"

// preferBundlerCacheMountFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const preferBundlerCacheMountFixPriority = 88

// PreferBundlerCacheMountRule flags `bundle install` invocations that don't
// use a BuildKit cache mount on `${BUNDLE_PATH}/cache`. Native-extension
// gems benefit most from the cache mount because they recompile from
// source on every cache-busted build.
type PreferBundlerCacheMountRule struct{}

// NewPreferBundlerCacheMountRule creates the rule.
func NewPreferBundlerCacheMountRule() *PreferBundlerCacheMountRule {
	return &PreferBundlerCacheMountRule{}
}

// Metadata returns the rule metadata.
func (r *PreferBundlerCacheMountRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferBundlerCacheMountRuleCode,
		Name:            "Prefer BuildKit cache mount for `bundle install`",
		Description:     "`bundle install` doesn't use a BuildKit cache mount; native-extension gems will recompile on every cache-busted build",
		DocURL:          rules.TallyDocURL(PreferBundlerCacheMountRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "performance",
		FixPriority:     preferBundlerCacheMountFixPriority,
	}
}

// Check runs the rule.
func (r *PreferBundlerCacheMountRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	// BuildKit cache mounts require the `# syntax=docker/dockerfile:1`
	// pragma. Without it, `--mount=type=cache` would error at build
	// time, so we don't recommend the rewrite.
	if !hasBuildKitDockerfileSyntax(input) {
		return nil
	}

	rubyFacts := input.Facts.RubyFacts()
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
		violations = append(violations, r.checkStage(input, sf, rubyFacts, meta)...)
	}
	return violations
}

func (r *PreferBundlerCacheMountRule) checkStage(
	input rules.LintInput,
	sf *facts.StageFacts,
	rubyFacts *rubyfacts.RubyFacts,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	for _, runFacts := range sf.Runs {
		if runFacts == nil || runFacts.Run == nil {
			continue
		}
		install := findFirstBundleInstall(runFacts)
		if install == nil {
			continue
		}
		if runHasBundlerCacheMount(runFacts) {
			continue
		}

		severity := meta.DefaultSeverity
		if rubyFacts != nil && rubyFacts.Lockfile != nil &&
			len(rubyFacts.Lockfile.NativeExtGems) > 0 {
			severity = rules.SeverityWarning
		}

		loc := bundleInstallViolationLocation(input.File, runFacts, *install, input.SourceMap())
		v := rules.NewViolation(loc, meta.Code, meta.Description, severity).
			WithDocURL(meta.DocURL).
			WithDetail(preferBundlerCacheMountDetail(rubyFacts))
		// FixSuggestion that doesn't auto-edit: rewriting --mount=type=cache
		// onto a multi-line RUN is too easy to get wrong (continuation
		// lines, existing --mount flags, heredoc bodies). The user
		// applies this manually.
		v = v.WithSuggestedFix(&rules.SuggestedFix{
			Description: "Add `--mount=type=cache,id=bundler,target=${BUNDLE_PATH}/cache,sharing=locked` to the bundle install RUN",
			Safety:      rules.FixSuggestion,
			Priority:    meta.FixPriority,
			IsPreferred: false,
		})
		violations = append(violations, v)
		// Report once per stage — multiple `bundle install` invocations
		// in a single stage almost always benefit from the same cache
		// mount, and the user only needs the recommendation once.
		return violations
	}
	return violations
}

func preferBundlerCacheMountDetail(rubyFacts *rubyfacts.RubyFacts) string {
	base := "BuildKit's `RUN --mount=type=cache,target=${BUNDLE_PATH}/cache` is the documented way to keep " +
		"the gem-extraction cache across builds. Without it, every cache-busted `bundle install` " +
		"re-fetches and (for native-extension gems) recompiles every gem from scratch — typically 30s+ on " +
		"a non-trivial Rails project."
	if rubyFacts != nil && rubyFacts.Lockfile != nil &&
		len(rubyFacts.Lockfile.NativeExtGems) > 0 {
		base += " This project resolves at least one native-extension gem (e.g. " +
			rubyFacts.Lockfile.NativeExtGems[0] +
			") whose recompile cost matters most."
	}
	return base
}

// runHasBundlerCacheMount reports whether the RUN has a
// `--mount=type=cache,target=${BUNDLE_PATH}/cache` flag (or a literal
// path equivalent).
func runHasBundlerCacheMount(runFacts *facts.RunFacts) bool {
	if runFacts == nil || runFacts.Run == nil {
		return false
	}
	mounts := runmount.GetMounts(runFacts.Run)
	for _, mount := range mounts {
		if mount == nil || mount.Type != "cache" {
			continue
		}
		if cacheTargetMatchesBundlerCache(mount.Target) {
			return true
		}
	}
	return false
}

// cacheTargetMatchesBundlerCache reports whether a cache-mount target
// path looks like a Bundler cache directory.
//
// Recognized shapes:
//
//	${BUNDLE_PATH}/cache
//	$BUNDLE_PATH/cache
//	/usr/local/bundle/cache       (Rails generator default)
//	/bundle/cache
//	/bundle-cache                  (commonly used name)
//	/cache                         (also seen)
//
// Other targets (e.g. /tmp) don't actually back Bundler's cache.
func cacheTargetMatchesBundlerCache(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	// Variable-expanded forms.
	if strings.Contains(target, "BUNDLE_PATH") && strings.HasSuffix(target, "/cache") {
		return true
	}
	// Exact-path forms commonly seen in the corpus.
	knownTargets := []string{
		"/usr/local/bundle/cache",
		"/bundle/cache",
		"/bundle-cache",
	}
	if slices.Contains(knownTargets, target) {
		return true
	}
	// `/cache` alone is too generic; require it to be the only target,
	// which is unlikely to be a false positive — but keep this loose.
	return target == "/cache"
}

// hasBuildKitDockerfileSyntax reports whether the Dockerfile's source
// carries the `# syntax=docker/dockerfile:1` (or higher) pragma. Cache
// mounts require BuildKit, which is enabled per-Dockerfile via this
// pragma.
func hasBuildKitDockerfileSyntax(input rules.LintInput) bool {
	if input.AST == nil {
		return false
	}
	// The pragma appears in the AST's directives.
	src := string(input.Source)
	for line := range strings.SplitSeq(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			// Stop at the first non-directive line.
			break
		}
		// Accept any `syntax=` directive that mentions dockerfile/labs;
		// we don't gate on a specific version because cache mounts
		// have been stable since the dockerfile/1 frontend.
		if strings.Contains(line, "syntax=") &&
			(strings.Contains(line, "docker/dockerfile") || strings.Contains(line, "dockerfile/labs")) {
			return true
		}
	}
	return false
}

func init() {
	rules.Register(NewPreferBundlerCacheMountRule())
}
