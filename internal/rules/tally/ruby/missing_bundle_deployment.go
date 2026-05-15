package ruby

import (
	"slices"
	"strings"

	rubyfacts "github.com/wharflab/tally/internal/facts/ruby"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/stagename"
)

// MissingBundleDeploymentRuleCode is the full rule code.
const MissingBundleDeploymentRuleCode = rules.TallyRulePrefix + "ruby/missing-bundle-deployment"

// missingBundleDeploymentFixPriority keeps this rule's edits ordered alongside
// the other Ruby production-hygiene rules. Same value as the jemalloc rule for
// predictable cross-rule sequencing.
const missingBundleDeploymentFixPriority = 88

// MissingBundleDeploymentRule flags production-shaped Ruby stages that run
// `bundle install` without `BUNDLE_DEPLOYMENT=1` (or the equivalent
// `bundle config set deployment 'true'`).
type MissingBundleDeploymentRule struct{}

// NewMissingBundleDeploymentRule creates the rule.
func NewMissingBundleDeploymentRule() *MissingBundleDeploymentRule {
	return &MissingBundleDeploymentRule{}
}

// Metadata returns the rule metadata.
func (r *MissingBundleDeploymentRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            MissingBundleDeploymentRuleCode,
		Name:            "Production bundle install must run in deployment mode",
		Description:     "Production stage runs bundle install without BUNDLE_DEPLOYMENT=1",
		DocURL:          rules.TallyDocURL(MissingBundleDeploymentRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		FixPriority:     missingBundleDeploymentFixPriority,
	}
}

// Check runs the rule.
func (r *MissingBundleDeploymentRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()
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
		if !stageLooksProduction(input, sf) {
			continue
		}
		violations = append(violations, r.checkStage(input, sf, sm, rubyFacts, meta)...)
	}
	return violations
}

func (r *MissingBundleDeploymentRule) checkStage(
	input rules.LintInput,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	rubyFacts *rubyfacts.RubyFacts,
	meta rules.RuleMetadata,
) []rules.Violation {
	// Compliance is evaluated at each `bundle install` invocation, not at
	// stage end. Docker ENV is forward-only and `bundle config set deployment`
	// only affects subsequent installs, so an env or config landing AFTER an
	// install can't retroactively make that install compliant.
	configBeforeIdx := false
	stageHasFrozenSignal := stageHasFrozenConfig(sf)
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		if !configBeforeIdx && runHasBundleConfigSetDeployment(runFacts) {
			configBeforeIdx = true
		}
		install := findFirstBundleInstall(runFacts)
		if install == nil {
			continue
		}
		// `bundle install --deployment` is the deprecated 2.x flag form;
		// the deprecated-flags rule will catch it separately, so for THIS
		// rule it counts as compliant.
		if installHasDeploymentFlag(*install) {
			continue
		}
		if envFactsContainBundleDeploymentSignal(runFacts.Env) {
			continue
		}
		if configBeforeIdx || runConfiguresBundleDeploymentBeforeInstall(runFacts, *install) {
			continue
		}

		severity := meta.DefaultSeverity
		// Refinement: when Gemfile.lock is not observable in the build
		// context, running `bundle install` in production is more dangerous
		// — Bundler resolves from Gemfile fresh on every build, so the
		// build is non-deterministic. Bump severity to error.
		if rubyFacts != nil && rubyFacts.Lockfile == nil {
			severity = rules.SeverityError
		}

		loc := bundleInstallViolationLocation(input.File, runFacts, *install, sm)
		v := rules.NewViolation(loc, meta.Code, meta.Description, severity).
			WithDocURL(meta.DocURL).
			WithDetail(missingBundleDeploymentDetail(stageHasFrozenSignal, severity))
		if fix := buildBundleDeploymentFix(input, sf, sm, meta.FixPriority); fix != nil {
			v = v.WithSuggestedFix(fix)
		}
		// Report once per stage even when the stage has multiple
		// `bundle install` invocations — the missing ENV is a stage-level
		// issue, not a per-RUN one.
		return []rules.Violation{v}
	}
	return nil
}

func missingBundleDeploymentDetail(hasFrozenSignal bool, severity rules.Severity) string {
	base := "Production stages that run `bundle install` should set " +
		"`ENV BUNDLE_DEPLOYMENT=\"1\"` (or `bundle config set --local deployment 'true'`). " +
		"Without it, Bundler may mutate `Gemfile.lock` at build time, install gems outside the project, " +
		"and skip the lockfile-required check — defeating the \"the lockfile is the build input\" property."
	if hasFrozenSignal {
		base += " The stage already runs `bundle config set frozen 'true'`, but `BUNDLE_DEPLOYMENT=1` is the " +
			"strict superset: it also pins `BUNDLE_PATH`, requires the lockfile to exist, and excludes dev/test gems."
	}
	if severity == rules.SeverityError {
		base += " No `Gemfile.lock` is observable in the build context — without one, Bundler resolves from " +
			"`Gemfile` fresh on every build and produces an indeterministic image."
	}
	return base
}

// envFactsContainBundleDeploymentSignal reports whether the env snapshot at
// a RUN site already declares `BUNDLE_DEPLOYMENT=1` (or any non-empty
// truthy value) via an ENV instruction. Only ENV-bound values count;
// build-time-only ARG values do not affect runtime install behaviour.
func envFactsContainBundleDeploymentSignal(env facts.EnvFacts) bool {
	value := strings.TrimSpace(env.Values["BUNDLE_DEPLOYMENT"])
	if value == "" || strings.EqualFold(value, "false") || value == "0" {
		return false
	}
	if _, ok := env.Bindings["BUNDLE_DEPLOYMENT"]; !ok {
		return false
	}
	return true
}

// runHasBundleConfigSetDeployment reports whether the RUN runs
// `bundle config set [--local|--global] deployment <truthy>`.
func runHasBundleConfigSetDeployment(runFacts *facts.RunFacts) bool {
	if runFacts == nil {
		return false
	}
	return slices.ContainsFunc(runFacts.CommandInfos, bundleConfigSetEnablesDeployment)
}

// runConfiguresBundleDeploymentBeforeInstall reports whether the RUN invokes
// `bundle config set ... deployment ...` at a command position *before* the
// `bundle install`. The position check uses the parsed CommandInfo source
// ranges — `bundle config set` and `bundle install` chained with `&&` in the
// same RUN both yield ordered CommandInfo entries.
func runConfiguresBundleDeploymentBeforeInstall(
	runFacts *facts.RunFacts,
	install shell.CommandInfo,
) bool {
	if runFacts == nil {
		return false
	}
	for _, ci := range runFacts.CommandInfos {
		if !bundleConfigSetEnablesDeployment(ci) {
			continue
		}
		if commandPrecedes(ci, install) {
			return true
		}
	}
	return false
}

// bundleConfigSetEnablesDeployment reports whether a CommandInfo represents
// a `bundle config set [--local|--global] deployment <truthy>` invocation.
//
// Recognized shapes (Bundler 2.x):
//
//	bundle config set deployment true
//	bundle config set deployment 'true'
//	bundle config set --local deployment true
//	bundle config set --global deployment 'true'
//
// The legacy 2-arg form (`bundle config deployment true`) is intentionally
// not recognized: Bundler 2 deprecated it in favor of `bundle config set ...`.
func bundleConfigSetEnablesDeployment(ci shell.CommandInfo) bool {
	return bundleConfigSetEnablesKey(ci, "deployment")
}

// bundleConfigSetEnablesFrozen reports whether a CommandInfo represents
// `bundle config set ... frozen <truthy>`.
func bundleConfigSetEnablesFrozen(ci shell.CommandInfo) bool {
	return bundleConfigSetEnablesKey(ci, "frozen")
}

// bundleConfigSetEnablesKey reports whether a CommandInfo represents
// `bundle config set [--local|--global] <key> <truthy>` for the given key.
// Truthy values: "true", "1" (case-insensitive, with surrounding quotes
// stripped). Used to detect deployment / frozen settings.
func bundleConfigSetEnablesKey(ci shell.CommandInfo, key string) bool {
	if !strings.EqualFold(ci.Name, "bundle") || !strings.EqualFold(ci.Subcommand, "config") {
		return false
	}
	args := argsAfterFirstMatch(ci.Args, ci.Subcommand)
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	if len(args) < 3 {
		return false
	}
	if !strings.EqualFold(args[0], "set") {
		return false
	}
	keyIdx := -1
	for i, a := range args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if strings.EqualFold(a, key) {
			keyIdx = i + 1
			break
		}
	}
	if keyIdx < 0 || keyIdx+1 >= len(args) {
		return false
	}
	value := strings.Trim(args[keyIdx+1], `"'`)
	return strings.EqualFold(value, "true") || value == "1"
}

// stageHasFrozenConfig reports whether any RUN in the stage runs
// `bundle config set ... frozen <truthy>`. Used only for fix wording —
// the rule still fires because frozen-only is not equivalent to deployment.
func stageHasFrozenConfig(sf *facts.StageFacts) bool {
	if sf == nil {
		return false
	}
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		if slices.ContainsFunc(runFacts.CommandInfos, bundleConfigSetEnablesFrozen) {
			return true
		}
	}
	return false
}

// installHasDeploymentFlag reports whether a parsed `bundle install` command
// carries the deprecated `--deployment` flag. The deprecated-flags rule will
// fire on it separately, but for THIS rule it counts as compliant.
func installHasDeploymentFlag(install shell.CommandInfo) bool {
	for _, a := range install.Args {
		if a == "--deployment" || strings.HasPrefix(a, "--deployment=") {
			return true
		}
	}
	return false
}

// buildBundleDeploymentFix emits the canonical Rails-generator-style fix:
// insert `ENV BUNDLE_DEPLOYMENT="1"` at the top of the stage. Insertion is
// zero-width at column 0 of the line immediately after the stage's `FROM`,
// matching the Rails 7.1 generator template's placement of `ENV BUNDLE_*`.
func buildBundleDeploymentFix(
	input rules.LintInput,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	priority int,
) *rules.SuggestedFix {
	if sf == nil || sm == nil {
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
		Description: `Add ENV BUNDLE_DEPLOYMENT="1" to enforce Bundler deployment-mode contract`,
		Safety:      rules.FixSafe,
		Priority:    priority,
		IsPreferred: true,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(input.File, insertLine, 0, insertLine, 0),
			NewText:  "ENV BUNDLE_DEPLOYMENT=\"1\"\n",
		}},
	}
}

func init() {
	rules.Register(NewMissingBundleDeploymentRule())
}
