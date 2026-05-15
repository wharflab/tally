package ruby

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// MissingBundleWithoutDevelopmentRuleCode is the full rule code.
const MissingBundleWithoutDevelopmentRuleCode = rules.TallyRulePrefix + "ruby/missing-bundle-without-development"

// missingBundleWithoutDevelopmentFixPriority keeps this rule's edits ordered
// alongside the other Ruby/PHP production-hygiene rules. Same value as
// jemalloc-installed-but-not-preloaded for predictable cross-rule sequencing.
const missingBundleWithoutDevelopmentFixPriority = 88

// productionRailsEnvKeys are the runtime-environment env vars that mark a
// Dockerfile as production-shaped when set to "production".
var productionRailsEnvKeys = []string{"RAILS_ENV", "RACK_ENV"}

// developmentGroupToken is the Bundler group name that production images
// must exclude. Matched as a substring of `BUNDLE_WITHOUT` (case-insensitive)
// per the design doc — the rule does not opinion-fight on whether `test` is
// also included.
const developmentGroupToken = "development"

// productionOnlyToken is the substring that signals `BUNDLE_ONLY` is scoping
// the install to production gem groups (Bundler 2.5+ inverse selector). When
// `BUNDLE_ONLY` carries this token, `BUNDLE_WITHOUT` is no longer required.
const productionOnlyToken = "production"

// MissingBundleWithoutDevelopmentRule flags production-shaped Ruby stages
// that run `bundle install` without excluding the `development` gem group.
type MissingBundleWithoutDevelopmentRule struct{}

// NewMissingBundleWithoutDevelopmentRule creates the rule.
func NewMissingBundleWithoutDevelopmentRule() *MissingBundleWithoutDevelopmentRule {
	return &MissingBundleWithoutDevelopmentRule{}
}

// Metadata returns the rule metadata.
func (r *MissingBundleWithoutDevelopmentRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            MissingBundleWithoutDevelopmentRuleCode,
		Name:            "Production bundle install must exclude the development group",
		Description:     "Production stage runs bundle install without BUNDLE_WITHOUT excluding the development group",
		DocURL:          rules.TallyDocURL(MissingBundleWithoutDevelopmentRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
		FixPriority:     missingBundleWithoutDevelopmentFixPriority,
	}
}

// Check runs the rule.
func (r *MissingBundleWithoutDevelopmentRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	// Refinement: when a Gemfile is observable and has no :development group,
	// the `BUNDLE_WITHOUT="development"` recommendation is moot — there is
	// nothing to exclude. Skip the rule entirely in that case so users with
	// library-style or ops-tool Gemfiles do not see noise. When the Gemfile
	// is not observable (Dockerfile-only mode) the rule applies.
	if rubyFacts := input.Facts.RubyFacts(); rubyFacts != nil &&
		rubyFacts.Gemfile != nil && !rubyFacts.Gemfile.HasDevGroup {
		return nil
	}

	sm := input.SourceMap()
	var violations []rules.Violation
	forEachRubyStage(input, func(_ int, _ instructions.Stage, sf *facts.StageFacts) {
		if !stageLooksProduction(input, sf) {
			return
		}
		violations = append(violations, r.checkStage(input, sf, sm, meta)...)
	})
	return violations
}

func (r *MissingBundleWithoutDevelopmentRule) checkStage(
	input rules.LintInput,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	// Compliance is evaluated at each `bundle install` invocation, not
	// at stage end. Docker `ENV` only affects subsequent instructions, and
	// `bundle config set` only affects subsequent installs — so an
	// `ENV BUNDLE_WITHOUT=development` (or `bundle config set ... without
	// development`) that lands in a RUN *after* the install is too late.
	configBeforeIdx := false
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		// Check whether *this* RUN already runs `bundle config set without
		// development`. The signal fires for any later `bundle install` in
		// the same stage, including one in a later RUN.
		if !configBeforeIdx && runHasBundleConfigSetWithoutDevelopment(runFacts) {
			configBeforeIdx = true
		}
		install := findFirstBundleInstall(runFacts)
		if install == nil {
			continue
		}
		// `runFacts.Env.Bindings` is the env *visible to this RUN*, so a
		// later `ENV BUNDLE_WITHOUT=development` is correctly invisible
		// here. Likewise `configBeforeIdx` is only true when an earlier RUN
		// (or the same RUN, before the install on the parsed-command level)
		// configured the exclusion.
		if envFactsContainBundleWithoutDevelopmentSignal(runFacts.Env) {
			continue
		}
		if configBeforeIdx || runConfiguresBundleWithoutDevelopmentBeforeInstall(runFacts, *install) {
			continue
		}
		loc := bundleInstallViolationLocation(input.File, runFacts, *install, sm)
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(missingBundleWithoutDevelopmentDetail())
		if fix := buildBundleWithoutDevelopmentFix(input, sf, sm, meta.FixPriority); fix != nil {
			v = v.WithSuggestedFix(fix)
		}
		// Report once per stage even when the stage has multiple
		// `bundle install` invocations — the missing ENV is a stage-level
		// issue, not a per-RUN one.
		return []rules.Violation{v}
	}
	return nil
}

func missingBundleWithoutDevelopmentDetail() string {
	return "Production stages that run `bundle install` should exclude the `development` gem group via " +
		"`ENV BUNDLE_WITHOUT=\"development\"` (or `bundle config set --local without development`, or " +
		"the Bundler 2.5+ inverse selector `ENV BUNDLE_ONLY=\"default:production\"`). " +
		"Otherwise gems like `web-console`, `byebug`, `pry`, `rspec-rails`, `letter_opener`, and `bullet` ship " +
		"into the production image, inflating size and exposing development-only attack surface " +
		"(`web-console` in particular has documented RCE history when it leaks into production)."
}

// stageLooksProduction reports whether the stage is shaped like a production
// runtime per the missing-bundle-deployment heuristic:
//
//  1. The stage's effective env (or any inherited base stage's env) binds
//     `RAILS_ENV` or `RACK_ENV` to "production" — strongest signal.
//  2. Otherwise, fall through to "default production" — the rule already
//     filters dev/test stages via `LooksLikeDev` and only fires on stages
//     that look like Ruby runtimes, so a stage that has reached this point
//     is implicitly production-shaped. This matches the missing-bundle-deployment
//     fallback ("the stage has no explicit non-production marker and the
//     final stage is shaped like an app runtime").
//
// Detection of an explicit non-production marker (e.g. `RAILS_ENV=development`
// in the stage's effective env) demotes the rule to silent. This guards
// against false positives on stages that explicitly opt out.
func stageLooksProduction(input rules.LintInput, sf *facts.StageFacts) bool {
	if sf == nil {
		return false
	}
	// Explicit non-production markers in the stage's effective env demote
	// the rule. Bundler treats `RAILS_ENV=production` as the runtime contract;
	// a stage that explicitly sets `development`/`test` is, by definition,
	// not production-shaped.
	for _, key := range productionRailsEnvKeys {
		value := strings.ToLower(strings.TrimSpace(envBoundValue(sf, key)))
		if value == "development" || value == "test" {
			return false
		}
	}
	// File-wide RAILS_ENV/RACK_ENV=production anywhere is the strongest
	// production signal.
	for stageIdx := range input.Stages {
		other := input.Facts.Stage(stageIdx)
		if other == nil {
			continue
		}
		for _, key := range productionRailsEnvKeys {
			if envValueEqualsProduction(envBoundValue(other, key)) {
				return true
			}
		}
	}
	// Default production-shape behaviour for any Ruby-runtime stage that
	// has not been filtered out by `LooksLikeDev`. Per the design doc:
	// "the stage has no explicit non-production marker and the final stage
	// is shaped like an app runtime".
	return true
}

func envValueEqualsProduction(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "production")
}

// envFactsContainBundleWithoutDevelopmentSignal reports whether the env
// snapshot at a RUN site already excludes the development group via
// `BUNDLE_WITHOUT` or `BUNDLE_ONLY`. Unlike a stage-final check, this uses
// the env state *visible to this RUN* — so an `ENV BUNDLE_WITHOUT=...` that
// lands in a later RUN does not retroactively make the earlier install
// compliant.
func envFactsContainBundleWithoutDevelopmentSignal(env facts.EnvFacts) bool {
	if envValueContainsToken(env.Values["BUNDLE_WITHOUT"], developmentGroupToken) {
		// Only count the value when it was bound by an `ENV` instruction
		// (not by a meta-ARG or other build-time-only mechanism).
		if _, ok := env.Bindings["BUNDLE_WITHOUT"]; ok {
			return true
		}
	}
	if envValueContainsToken(env.Values["BUNDLE_ONLY"], productionOnlyToken) {
		if _, ok := env.Bindings["BUNDLE_ONLY"]; ok {
			return true
		}
	}
	return false
}

// runHasBundleConfigSetWithoutDevelopment reports whether the RUN runs
// `bundle config set [--local|--global] without ...development...` at all.
// Used to mark the RUN's effect as "configured exclusion" so any subsequent
// `bundle install` in the same stage (in this RUN or a later one) is
// compliant.
func runHasBundleConfigSetWithoutDevelopment(runFacts *facts.RunFacts) bool {
	if runFacts == nil {
		return false
	}
	return slices.ContainsFunc(runFacts.CommandInfos, bundleConfigSetExcludesDevelopment)
}

// runConfiguresBundleWithoutDevelopmentBeforeInstall reports whether the
// same RUN invokes `bundle config set ... without ... development` at a
// command position *before* the `bundle install`. The position check uses
// the parsed CommandInfo source ranges — `bundle config set` and
// `bundle install` chained with `&&` in the same RUN both yield ordered
// CommandInfo entries.
func runConfiguresBundleWithoutDevelopmentBeforeInstall(
	runFacts *facts.RunFacts,
	install shell.CommandInfo,
) bool {
	if runFacts == nil {
		return false
	}
	for _, ci := range runFacts.CommandInfos {
		if !bundleConfigSetExcludesDevelopment(ci) {
			continue
		}
		if commandPrecedes(ci, install) {
			return true
		}
	}
	return false
}

// commandPrecedes reports whether command `a` appears before command `b`
// in the parsed source script. Position comparison uses (Line, StartCol)
// from each CommandInfo. Used to enforce ordering on shell-level command
// chains within a single RUN.
func commandPrecedes(a, b shell.CommandInfo) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.StartCol < b.StartCol
}

// envValueContainsToken reports whether a Bundler env value contains the
// supplied token as a substring (case-insensitive). Bundler accepts colon-,
// space-, or comma-separated group lists; substring matching is sufficient
// because the token names ("development", "production") do not appear inside
// other meaningful group names.
func envValueContainsToken(value, token string) bool {
	if value == "" {
		return false
	}
	return strings.Contains(strings.ToLower(value), token)
}

// bundleConfigSetExcludesDevelopment reports whether a CommandInfo represents
// a `bundle config set [--local|--global] without ...` invocation whose value
// list contains "development".
//
// Recognized shapes (Bundler 2.x):
//
//	bundle config set without development
//	bundle config set without 'development test'
//	bundle config set --local without development:test
//	bundle config set --global without development
//	bundle config set --local without "development test"
//
// The legacy 2-arg form (`bundle config without development`) is intentionally
// not recognized: Bundler 2 deprecated it in favor of `bundle config set ...`.
func bundleConfigSetExcludesDevelopment(ci shell.CommandInfo) bool {
	if !strings.EqualFold(ci.Name, "bundle") || !strings.EqualFold(ci.Subcommand, "config") {
		return false
	}
	args := argsAfterFirstMatch(ci.Args, ci.Subcommand)
	// Skip leading flags (`--local`, `--global`, ...).
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	// Expect `set without <values...>`.
	if len(args) < 3 {
		return false
	}
	if !strings.EqualFold(args[0], "set") {
		return false
	}
	withoutIdx := -1
	for i, a := range args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if strings.EqualFold(a, "without") {
			withoutIdx = i + 1
			break
		}
	}
	if withoutIdx < 0 || withoutIdx+1 >= len(args) {
		return false
	}
	for _, value := range args[withoutIdx+1:] {
		if envValueContainsToken(value, developmentGroupToken) {
			return true
		}
	}
	return false
}

// argsAfterFirstMatch returns the slice of args that come after the first
// case-insensitive occurrence of needle. Returns the original slice when no
// match is found, which keeps callers' fallback paths simple.
func argsAfterFirstMatch(args []string, needle string) []string {
	for i, a := range args {
		if strings.EqualFold(a, needle) {
			return args[i+1:]
		}
	}
	return args
}

// findFirstBundleInstall returns the first parsed `bundle install` command
// in the run, or nil when none is present. We return the first match
// because the rule reports once per stage and the violation location is
// attributed to the first invocation.
func findFirstBundleInstall(runFacts *facts.RunFacts) *shell.CommandInfo {
	for i := range runFacts.CommandInfos {
		ci := runFacts.CommandInfos[i]
		if isBundleInstall(ci) {
			return &runFacts.CommandInfos[i]
		}
	}
	return nil
}

func isBundleInstall(ci shell.CommandInfo) bool {
	return strings.EqualFold(ci.Name, "bundle") && strings.EqualFold(ci.Subcommand, "install")
}

// bundleInstallViolationLocation prefers the precise subcommand position
// when available, falling back to the RUN's first range.
func bundleInstallViolationLocation(
	file string,
	runFacts *facts.RunFacts,
	cmd shell.CommandInfo,
	sm *sourcemap.SourceMap,
) rules.Location {
	if runFacts == nil || runFacts.Run == nil {
		return rules.NewFileLocation(file)
	}
	runRanges := runFacts.Run.Location()
	if len(runRanges) == 0 {
		return rules.NewFileLocation(file)
	}
	if cmd.SubcommandLine >= 0 && cmd.SubcommandStartCol >= 0 && cmd.SubcommandEndCol > cmd.SubcommandStartCol {
		line := runRanges[0].Start.Line + cmd.SubcommandLine
		startCol, endCol := cmd.SubcommandStartCol, cmd.SubcommandEndCol
		if cmd.SubcommandLine == 0 && sm != nil {
			offset := shell.DockerfileRunCommandStartCol(sm.Line(line - 1))
			startCol += offset
			endCol += offset
		}
		return rules.NewRangeLocation(file, line, startCol, line, endCol)
	}
	return rules.NewLocationFromRanges(file, runRanges)
}

// buildBundleWithoutDevelopmentFix emits the canonical Rails-generator-style
// fix: insert `ENV BUNDLE_WITHOUT="..."` at the top of the stage. The exact
// value depends on observable Gemfile state:
//
//   - both :development and :test groups present  → "development:test"
//   - only :development present                   → "development"
//   - Gemfile not observable                      → "development" (default)
//
// The insertion point is the line immediately after the stage's `FROM`,
// matching the Rails 7.1 generator template's placement of `ENV BUNDLE_*`.
// Insertion is zero-width at column 0 of that line, so it composes cleanly
// with other rule edits in the same stage.
func buildBundleWithoutDevelopmentFix(
	input rules.LintInput,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	priority int,
) *rules.SuggestedFix {
	if sm == nil {
		return nil
	}
	value := chooseBundleWithoutValue(input)
	return buildStageTopEnvFix(
		input, sf, priority,
		`ENV BUNDLE_WITHOUT="`+value+`"`,
		`Add ENV BUNDLE_WITHOUT="`+value+`" to exclude development gems from the production image`,
		true,
	)
}

// chooseBundleWithoutValue picks the BUNDLE_WITHOUT value the fix should
// emit. When a Gemfile is observable and contains both `:development` and
// `:test` groups, the recommendation is the broader `development:test`
// exclusion so production images don't ship `rspec-rails`/`capybara`/etc.
// either. Otherwise the canonical Rails-generator value `development`
// applies.
func chooseBundleWithoutValue(input rules.LintInput) string {
	if rubyFacts := input.Facts.RubyFacts(); rubyFacts != nil && rubyFacts.Gemfile != nil {
		if rubyFacts.Gemfile.HasDevGroup && rubyFacts.Gemfile.HasTestGroup {
			return "development:test"
		}
	}
	return "development"
}

func init() {
	rules.Register(NewMissingBundleWithoutDevelopmentRule())
}
