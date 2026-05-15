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
			for flag, envVar := range deprecatedBundleInstallFlags {
				value, present := bundleInstallFlagValue(ci, flag)
				if !present {
					continue
				}
				loc := bundleInstallViolationLocation(input.File, runFacts, ci, sm)
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

// bundleInstallFlagValue reports whether the parsed bundle install
// command uses flag, and returns its value (may be empty for `--deployment`
// which takes no value).
//
// Recognized shapes:
//
//	--without development               (followed-by-value form)
//	--without "development test"
//	--without=development                (=-attached form)
//	--deployment                         (no value)
//	--path vendor/bundle
//	--path=vendor/bundle
func bundleInstallFlagValue(ci shell.CommandInfo, flag string) (string, bool) {
	for i, a := range ci.Args {
		if a == flag {
			// Followed-by-value form. `--deployment` takes no value;
			// for that flag we don't care about the next token.
			if flag == flagDeployment {
				return "", true
			}
			if i+1 < len(ci.Args) {
				return strings.Trim(ci.Args[i+1], `"'`), true
			}
			return "", true
		}
		if strings.HasPrefix(a, flag+"=") {
			return strings.Trim(a[len(flag)+1:], `"'`), true
		}
	}
	return "", false
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
