package ruby

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/stagename"
)

// PreferSecretMountsForBuildCredentialsRuleCode is the full rule code.
const PreferSecretMountsForBuildCredentialsRuleCode = rules.TallyRulePrefix + "ruby/prefer-secret-mounts-for-build-credentials"

// preferSecretMountsForBuildCredentialsFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const preferSecretMountsForBuildCredentialsFixPriority = 88

// rubyPrivateGemCredentialEnvKeys maps Ruby/Bundler ecosystem
// build-credential env-var names to a canonical secret-mount id.
//
// `BUNDLE_<HOST>__<TLD>` is Bundler's host-credential pattern (e.g.
// BUNDLE_GITHUB__COM, BUNDLE_GITLAB__COM, BUNDLE_GEMS__MYCOMPANY__COM).
// We recognize a few well-known forms explicitly; any ARG/ENV name
// matching `BUNDLE_*__*` is treated as a Bundler host credential by
// the trigger logic.
var rubyPrivateGemCredentialEnvKeys = map[string]string{ //nolint:gosec // env-var names, not credential values
	"BUNDLE_GITHUB__COM":    "github_token",
	"BUNDLE_BITBUCKET__ORG": "bitbucket_token",
	"BUNDLE_GITLAB__COM":    "gitlab_token",
	"GEM_HOST_API_KEY":      "rubygems_api_key",
	"NPM_TOKEN":             "npm_token",
	"YARN_AUTH_TOKEN":       "yarn_auth_token",
}

// PreferSecretMountsForBuildCredentialsRule is the constructive
// companion to tally/ruby/secrets-in-arg-or-env: when a Ruby Dockerfile
// declares a build-credential env var via ARG or ENV, this rule
// surfaces the BuildKit secret-mount alternative as the supported way
// to thread the credential through the build without leaking it into
// image cache key data or layer history.
type PreferSecretMountsForBuildCredentialsRule struct{}

// NewPreferSecretMountsForBuildCredentialsRule creates the rule.
func NewPreferSecretMountsForBuildCredentialsRule() *PreferSecretMountsForBuildCredentialsRule {
	return &PreferSecretMountsForBuildCredentialsRule{}
}

// Metadata returns the rule metadata.
func (r *PreferSecretMountsForBuildCredentialsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferSecretMountsForBuildCredentialsRuleCode,
		Name:            "Prefer BuildKit secret mounts for build-time credentials",
		Description:     "Build-time credential declared via ARG/ENV; prefer `RUN --mount=type=secret,...`",
		DocURL:          rules.TallyDocURL(PreferSecretMountsForBuildCredentialsRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "security",
		FixPriority:     preferSecretMountsForBuildCredentialsFixPriority,
	}
}

// Check runs the rule.
func (r *PreferSecretMountsForBuildCredentialsRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	var violations []rules.Violation

	// Meta-ARG (before any FROM) only counts when the Dockerfile has at
	// least one Ruby-shaped stage — otherwise this is a generic
	// Node.js/Python/etc. Dockerfile that happens to declare an env var
	// with one of the names we recognize, and the rule's Ruby-specific
	// recommendation doesn't apply.
	if dockerfileHasRubyStage(input) {
		for _, arg := range input.MetaArgs {
			violations = append(violations, r.checkArg(input, &arg, meta)...)
		}
	}

	for stageIdx, stage := range input.Stages {
		if stagename.LooksLikeDev(stage.Name) {
			continue
		}
		if input.Facts == nil {
			continue
		}
		sf := input.Facts.Stage(stageIdx)
		if sf == nil {
			continue
		}
		if sf.BaseImageOS == semantic.BaseImageOSWindows {
			continue
		}
		// Gate per-stage checks on the Ruby-shape signal so this rule's
		// Ruby-specific recommendation doesn't fire on Node/Python/etc.
		// Dockerfiles that happen to use one of the env-var names.
		if !stageLooksLikeRuby(input.Semantic, stageIdx, stage, sf) {
			continue
		}
		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.ArgCommand:
				violations = append(violations, r.checkArg(input, c, meta)...)
			case *instructions.EnvCommand:
				violations = append(violations, r.checkEnv(input, c, meta)...)
			}
		}
	}
	return violations
}

// dockerfileHasRubyStage reports whether at least one stage in the
// Dockerfile is Ruby-shaped AND would not be filtered out of the
// per-stage scan — used to gate meta-ARG checks (which run before any
// FROM and so can't be tied to a specific stage). Without this, a
// Dockerfile that has only `dev`/`test` Ruby stages would still emit
// meta-ARG findings even though every per-stage check would skip.
func dockerfileHasRubyStage(input rules.LintInput) bool {
	if input.Facts == nil {
		return false
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
		if stageLooksLikeRuby(input.Semantic, stageIdx, stage, sf) {
			return true
		}
	}
	return false
}

func (r *PreferSecretMountsForBuildCredentialsRule) checkArg(
	input rules.LintInput,
	cmd *instructions.ArgCommand,
	meta rules.RuleMetadata,
) []rules.Violation {
	if cmd == nil {
		return nil
	}
	keys := make([]string, 0, len(cmd.Args))
	for _, arg := range cmd.Args {
		keys = append(keys, arg.Key)
	}
	return r.emitViolations(input, cmd.Location(), keys, strings.ToUpper(command.Arg), meta)
}

func (r *PreferSecretMountsForBuildCredentialsRule) checkEnv(
	input rules.LintInput,
	cmd *instructions.EnvCommand,
	meta rules.RuleMetadata,
) []rules.Violation {
	if cmd == nil {
		return nil
	}
	keys := make([]string, 0, len(cmd.Env))
	for _, kv := range cmd.Env {
		keys = append(keys, kv.Key)
	}
	return r.emitViolations(input, cmd.Location(), keys, strings.ToUpper(command.Env), meta)
}

// emitViolations builds one violation per recognized credential env-var
// key in keys. Common to both ARG and ENV — the only difference is the
// instruction label that appears in the user-visible detail message.
func (r *PreferSecretMountsForBuildCredentialsRule) emitViolations(
	input rules.LintInput,
	cmdLoc []parser.Range,
	keys []string,
	instruction string,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	for _, key := range keys {
		secretID, ok := matchRubyBuildCredentialKey(key)
		if !ok {
			continue
		}
		loc := rules.NewLocationFromRanges(input.File, cmdLoc)
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(preferSecretMountsDetail(key, secretID, instruction)).
			WithSuggestedFix(buildSecretMountFix(key, secretID, meta.FixPriority))
		violations = append(violations, v)
	}
	return violations
}

// matchRubyBuildCredentialKey reports whether key is a recognized Ruby
// build-credential env var, returning the canonical secret-mount id.
//
// Recognition order:
//  1. Explicit map (well-known forms).
//  2. BUNDLE_<HOST>__<TLD> pattern (Bundler's host-credential
//     convention — host name with dots replaced by `__`).
func matchRubyBuildCredentialKey(key string) (string, bool) {
	if id, ok := rubyPrivateGemCredentialEnvKeys[key]; ok {
		return id, true
	}
	if isBundlerHostCredentialKey(key) {
		return strings.ToLower(key), true
	}
	return "", false
}

// bundlerHostCredentialTLDs is the set of TLDs that, when they appear
// as the trailing `__<TLD>` segment of a `BUNDLE_*` key, signal a
// host-credential rather than a generic dotted config key.
//
// Bundler's `__` is overloaded — it encodes `.` for both
// `BUNDLE_GITHUB__COM` (a credential) AND `BUNDLE_LOCAL__RACK` (a
// config key for `local.rack`). Trailing-TLD discrimination is the
// least bad heuristic without a registry of known config keys. We err
// on the side of completeness and include the public-suffix TLDs
// commonly seen on gem-host URLs.
var bundlerHostCredentialTLDs = map[string]bool{
	"COM": true,
	"ORG": true,
	"NET": true,
	"IO":  true,
	"DEV": true,
	"CO":  true,
	"AI":  true,
}

// isBundlerHostCredentialKey reports whether a key matches Bundler's
// `BUNDLE_<HOST>__<TLD>` host-credential convention. Host names use
// `__` (double underscore) for `.` (dot), so a real Bundler key has at
// least one `__` separator AND ends with a known TLD segment to
// distinguish from configuration keys like `BUNDLE_LOCAL__RACK`
// (Bundler's `local.rack` config).
func isBundlerHostCredentialKey(key string) bool {
	if !strings.HasPrefix(key, "BUNDLE_") {
		return false
	}
	rest := strings.TrimPrefix(key, "BUNDLE_")
	if !strings.Contains(rest, "__") {
		return false
	}
	// The trailing `__<TLD>` segment must be a recognized public-suffix
	// TLD. Take the last `__`-separated token.
	idx := strings.LastIndex(rest, "__")
	tld := rest[idx+2:]
	return bundlerHostCredentialTLDs[tld]
}

func preferSecretMountsDetail(envKey, secretID, instruction string) string {
	return "`" + envKey + "` is a build-time credential; declaring it via " + instruction + " bakes the " +
		"value into image history (`docker history --no-trunc <image>`) and into the build cache key " +
		"data. Pass it through a BuildKit secret mount instead — the secret exists for the duration of " +
		"the RUN it's mounted into and never enters image content or cache key data: " +
		"`RUN --mount=type=secret,id=" + secretID + ",env=" + envKey + " <command>`."
}

// buildSecretMountFix emits a non-edit suggestion. The exact rewrite
// depends on the user's CI/secret-injection mechanism, so the rule
// can't auto-rewrite.
func buildSecretMountFix(envKey, secretID string, priority int) *rules.SuggestedFix {
	return &rules.SuggestedFix{
		Description: "Replace ARG/ENV " + envKey + " with `RUN --mount=type=secret,id=" + secretID +
			",env=" + envKey + " <command>`. Pass the secret value at build time via " +
			"`docker buildx build --secret id=" + secretID + ",src=...`.",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: false,
	}
}

func init() {
	rules.Register(NewPreferSecretMountsForBuildCredentialsRule())
}
