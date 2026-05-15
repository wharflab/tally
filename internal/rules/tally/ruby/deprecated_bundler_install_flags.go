package ruby

import (
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/stagename"
)

// DeprecatedBundlerInstallFlagsRuleCode is the full rule code.
const DeprecatedBundlerInstallFlagsRuleCode = rules.TallyRulePrefix + "ruby/deprecated-bundler-install-flags"

// deprecatedBundlerInstallFlagsFixPriority keeps this rule's edits ordered
// alongside the other Ruby rules.
const deprecatedBundlerInstallFlagsFixPriority = 88

// Deprecated `bundle install` flags (Bundler 2.x).
//
// flagWithout / flagDeployment / flagPath are the lookup keys for
// deprecatedBundleInstallFlags and the search tokens in
// bundleInstallFlagValue. Pulled into named constants because they
// appear repeatedly across this file.
const (
	flagWithout    = "--without"
	flagDeployment = "--deployment"
	flagPath       = "--path"
)

// deprecatedBundleInstallFlags lists the flag names Bundler 2.x has
// retired in favor of `BUNDLE_*` env vars and `bundle config set`.
// Each entry maps flag → recommended ENV var.
var deprecatedBundleInstallFlags = map[string]string{
	flagWithout:    "BUNDLE_WITHOUT",
	flagDeployment: "BUNDLE_DEPLOYMENT",
	flagPath:       "BUNDLE_PATH",
}

// DeprecatedBundlerInstallFlagsRule flags `bundle install` invocations
// using flags that Bundler 2.x deprecated (--without, --deployment,
// --path). They still work but emit deprecation notices on every CI
// build and are slated for removal in Bundler 3.
type DeprecatedBundlerInstallFlagsRule struct{}

// NewDeprecatedBundlerInstallFlagsRule creates the rule.
func NewDeprecatedBundlerInstallFlagsRule() *DeprecatedBundlerInstallFlagsRule {
	return &DeprecatedBundlerInstallFlagsRule{}
}

// Metadata returns the rule metadata.
func (r *DeprecatedBundlerInstallFlagsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            DeprecatedBundlerInstallFlagsRuleCode,
		Name:            "Avoid deprecated `bundle install` flags",
		Description:     "`bundle install` uses a flag deprecated in Bundler 2.x (--without, --deployment, --path)",
		DocURL:          rules.TallyDocURL(DeprecatedBundlerInstallFlagsRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		FixPriority:     deprecatedBundlerInstallFlagsFixPriority,
	}
}

// Check runs the rule.
func (r *DeprecatedBundlerInstallFlagsRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

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
		violations = append(violations, r.checkStage(input, sf, sm, meta)...)
	}
	return violations
}

func (r *DeprecatedBundlerInstallFlagsRule) checkStage(
	input rules.LintInput,
	sf *facts.StageFacts,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	for _, runFacts := range sf.Runs {
		if runFacts == nil {
			continue
		}
		for _, ci := range runFacts.CommandInfos {
			if !isBundleInstall(ci) {
				continue
			}
			// Iterate flags in deterministic source-order so multiple
			// deprecated flags in one command produce stable, distinct
			// violations (a map iteration would be non-deterministic
			// AND attribute all violations to the same source location,
			// which the CLI then dedups down to one).
			seen := make(map[string]bool, len(ci.Args))
			for argIdx, arg := range ci.Args {
				flag := matchedDeprecatedFlag(arg)
				if flag == "" {
					continue
				}
				if seen[flag] {
					continue
				}
				seen[flag] = true
				value, present := bundleInstallFlagValue(ci, flag)
				if !present {
					continue
				}
				envVar := deprecatedBundleInstallFlags[flag]
				loc := flagLocationOrFallback(input.File, runFacts, ci, argIdx, sm)
				v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
					WithDocURL(meta.DocURL).
					WithDetail(deprecatedFlagDetail(flag, envVar))
				v = v.WithSuggestedFix(buildDeprecatedFlagFix(flag, envVar, value, meta.FixPriority))
				violations = append(violations, v)
			}
		}
	}
	return violations
}

// matchedDeprecatedFlag returns the canonical flag name when arg is one
// of the recognized deprecated flags (in either bare form like `--without`
// or =-attached form like `--without=development`). Returns "" otherwise.
func matchedDeprecatedFlag(arg string) string {
	for flag := range deprecatedBundleInstallFlags {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return flag
		}
	}
	return ""
}

// flagLocationOrFallback returns the source range of the flag arg at
// argIdx within the CommandInfo when the parser preserved arg ranges,
// otherwise the bundle install command's source range. The per-arg
// location lets multi-flag invocations (`--without dev --deployment`)
// produce distinct violation locations that survive the CLI's
// (file,line,col,rule) deduplication.
func flagLocationOrFallback(
	file string,
	runFacts *facts.RunFacts,
	ci shell.CommandInfo,
	argIdx int,
	sm *sourcemap.SourceMap,
) rules.Location {
	if runFacts == nil || runFacts.Run == nil {
		return rules.NewFileLocation(file)
	}
	runRanges := runFacts.Run.Location()
	if len(runRanges) == 0 {
		return rules.NewFileLocation(file)
	}
	if argIdx < 0 || argIdx >= len(ci.ArgRanges) {
		return bundleInstallViolationLocation(file, runFacts, ci, sm)
	}
	rng := ci.ArgRanges[argIdx]
	line := runRanges[0].Start.Line + rng.Line
	startCol := rng.StartCol
	endCol := rng.EndCol
	// Translate from script-relative to Dockerfile-relative coordinates
	// when the arg is on the first script line (the `RUN <flags>` prefix
	// has been stripped from SourceScript).
	if rng.Line == 0 && sm != nil {
		offset := shell.DockerfileRunCommandStartCol(sm.Line(line - 1))
		startCol += offset
		endCol += offset
	}
	return rules.NewRangeLocation(file, line, startCol, line, endCol)
}

// bundleInstallFlagValue reports whether the parsed bundle install
// command uses flag, and returns its value (may be empty for `--deployment`
// which takes no value).
//
// Recognized shapes:
//
//	--without development               (followed-by-value form)
//	--without "development test"        (quoted multi-group)
//	--without development test          (multi-arg form)
//	--without=development                (=-attached form)
//	--deployment                         (no value)
//	--path vendor/bundle
//	--path=vendor/bundle
//
// For multi-arg forms (`--without development test`), the function
// concatenates the value tokens with spaces — this matches what Bundler
// actually does and what the env-var equivalent (`BUNDLE_WITHOUT="development test"`)
// expects.
//
// The function is careful to NOT treat the next token as the flag's
// value when it starts with `-` (i.e. it's another flag). Without that
// guard, `--without --deployment` would think `--deployment` is the
// `--without` value.
func bundleInstallFlagValue(ci shell.CommandInfo, flag string) (string, bool) {
	for i, a := range ci.Args {
		if a == flag {
			// Followed-by-value form. `--deployment` takes no value;
			// for that flag we don't care about the next token.
			if flag == flagDeployment {
				return "", true
			}
			values := collectFlagValues(ci.Args[i+1:])
			return strings.Join(values, " "), true
		}
		if strings.HasPrefix(a, flag+"=") {
			return strings.Trim(a[len(flag)+1:], `"'`), true
		}
	}
	return "", false
}

// collectFlagValues collects consecutive non-flag arg tokens, stripping
// surrounding quotes. Stops at the first arg that starts with `-` (the
// next flag) or at end of args.
func collectFlagValues(args []string) []string {
	var values []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			break
		}
		values = append(values, strings.Trim(a, `"'`))
	}
	return values
}

func deprecatedFlagDetail(flag, envVar string) string {
	return "Bundler 2.x deprecated `bundle install " + flag + "` in favor of `ENV " + envVar + "=...` " +
		"(or `bundle config set " + strings.TrimPrefix(envVar, "BUNDLE_") + " ...`). The flag still works " +
		"but emits a deprecation notice on every CI run, and is slated for removal in Bundler 3."
}

// buildDeprecatedFlagFix proposes the env-var replacement. Safety is
// FixSafe for `--without <groups>` and `--deployment` (the env-var form
// is functionally equivalent), and FixSuggestion for `--path` (the user
// may have a downstream BUNDLE_PATH expectation that depends on the
// current setup).
func buildDeprecatedFlagFix(flag, envVar, value string, priority int) *rules.SuggestedFix {
	safety := rules.FixSafe
	envValue := value
	switch flag {
	case flagDeployment:
		envValue = "1"
	case flagPath:
		safety = rules.FixSuggestion
	}
	return &rules.SuggestedFix{
		Description: "Replace `" + flag + "` with `ENV " + envVar + `="` + envValue + `"` +
			"` (Bundler 2.x prefers the env-var form)",
		Safety:      safety,
		Priority:    priority,
		IsPreferred: false,
	}
}

func init() {
	rules.Register(NewDeprecatedBundlerInstallFlagsRule())
}
