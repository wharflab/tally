package ruby

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

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

	for _, arg := range input.MetaArgs {
		if v := r.checkArg(input, &arg, meta); v != nil {
			violations = append(violations, *v)
		}
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
		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.ArgCommand:
				if v := r.checkArg(input, c, meta); v != nil {
					violations = append(violations, *v)
				}
			case *instructions.EnvCommand:
				violations = append(violations, r.checkEnv(input, c, meta)...)
			}
		}
	}
	return violations
}

func (r *PreferSecretMountsForBuildCredentialsRule) checkArg(
	input rules.LintInput,
	cmd *instructions.ArgCommand,
	meta rules.RuleMetadata,
) *rules.Violation {
	if cmd == nil {
		return nil
	}
	for _, arg := range cmd.Args {
		secretID, ok := matchRubyBuildCredentialKey(arg.Key)
		if !ok {
			continue
		}
		loc := rules.NewLocationFromRanges(input.File, cmd.Location())
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(preferSecretMountsDetail(arg.Key, secretID, strings.ToUpper(command.Arg))).
			WithSuggestedFix(buildSecretMountFix(arg.Key, secretID, meta.FixPriority))
		return &v
	}
	return nil
}

func (r *PreferSecretMountsForBuildCredentialsRule) checkEnv(
	input rules.LintInput,
	cmd *instructions.EnvCommand,
	meta rules.RuleMetadata,
) []rules.Violation {
	if cmd == nil {
		return nil
	}
	var violations []rules.Violation
	for _, kv := range cmd.Env {
		secretID, ok := matchRubyBuildCredentialKey(kv.Key)
		if !ok {
			continue
		}
		loc := rules.NewLocationFromRanges(input.File, cmd.Location())
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(preferSecretMountsDetail(kv.Key, secretID, strings.ToUpper(command.Env))).
			WithSuggestedFix(buildSecretMountFix(kv.Key, secretID, meta.FixPriority))
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

// isBundlerHostCredentialKey reports whether a key matches Bundler's
// `BUNDLE_<HOST>__<TLD>` host-credential convention. Host names use
// `__` (double underscore) for `.` (dot), so a real Bundler key has at
// least one `__` separator.
func isBundlerHostCredentialKey(key string) bool {
	if !strings.HasPrefix(key, "BUNDLE_") {
		return false
	}
	// Must have at least one `__` after the BUNDLE_ prefix to look like
	// a host (dot-separated) credential.
	rest := strings.TrimPrefix(key, "BUNDLE_")
	return strings.Contains(rest, "__")
}

func preferSecretMountsDetail(envKey, secretID, instruction string) string {
	return "`" + envKey + "` is a build-time credential; declaring it via " + instruction + " bakes the " +
		"value into image history (`docker history --no-trunc <image>`) and into the build cache key " +
		"data. Pass it through a BuildKit secret mount instead — the secret exists for the duration of " +
		"the RUN it's mounted into and never enters image content or cache key data: " +
		"`RUN --mount=type=secret,id=" + secretID + ",env=" + envKey + " bundle install`."
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
