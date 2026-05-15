package ruby

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/stagename"
)

// SecretsInArgOrEnvRuleCode is the full rule code.
const SecretsInArgOrEnvRuleCode = rules.TallyRulePrefix + "ruby/secrets-in-arg-or-env"

// secretsInArgOrEnvFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const secretsInArgOrEnvFixPriority = 88

// railsSecretEnvKeys are the env-var names known to carry Rails-app secrets.
// Setting them via ARG/ENV at build time bakes the secret into image
// history (visible via `docker history --no-trunc`).
var railsSecretEnvKeys = map[string]bool{
	"SECRET_KEY_BASE":   true,
	"RAILS_MASTER_KEY":  true,
	"SECRET_TOKEN":      true, // legacy Rails 4.x
	"DEVISE_SECRET_KEY": true,
	"DEVISE_PEPPER":     true,
	"RAILS_KEY":         true, // some apps use this name as an alias
}

// SecretsInArgOrEnvRule flags ARG/ENV instructions that declare known
// Rails-app secret env vars with non-placeholder values.
type SecretsInArgOrEnvRule struct{}

// NewSecretsInArgOrEnvRule creates the rule.
func NewSecretsInArgOrEnvRule() *SecretsInArgOrEnvRule {
	return &SecretsInArgOrEnvRule{}
}

// Metadata returns the rule metadata.
func (r *SecretsInArgOrEnvRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            SecretsInArgOrEnvRuleCode,
		Name:            "Rails secrets must not be passed via ARG or ENV",
		Description:     "Rails secret declared via ARG/ENV bakes the secret into image history",
		DocURL:          rules.TallyDocURL(SecretsInArgOrEnvRuleCode),
		DefaultSeverity: rules.SeverityError,
		Category:        "security",
		FixPriority:     secretsInArgOrEnvFixPriority,
	}
}

// Check runs the rule.
func (r *SecretsInArgOrEnvRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	var violations []rules.Violation

	// Check meta-ARGs (those before any FROM).
	for _, arg := range input.MetaArgs {
		if v := r.checkArgValue(input, &arg, meta); v != nil {
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
		// Walk all ARG and ENV commands in the stage.
		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.ArgCommand:
				if v := r.checkArgValue(input, c, meta); v != nil {
					violations = append(violations, *v)
				}
			case *instructions.EnvCommand:
				violations = append(violations, r.checkEnvValues(input, c, meta)...)
			}
		}
	}
	return violations
}

func (r *SecretsInArgOrEnvRule) checkArgValue(
	input rules.LintInput,
	cmd *instructions.ArgCommand,
	meta rules.RuleMetadata,
) *rules.Violation {
	if cmd == nil {
		return nil
	}
	for _, arg := range cmd.Args {
		if !railsSecretEnvKeys[arg.Key] {
			continue
		}
		// ARG with no default value is still suspicious — it becomes
		// part of build cache key data and the user is meant to pass a
		// real secret via --build-arg, which would then leak into image
		// history once an ENV/RUN consumes it.
		var value string
		if arg.Value != nil {
			value = strings.TrimSpace(*arg.Value)
		}
		if isPlaceholderSecretValue(value) {
			continue
		}
		loc := rules.NewLocationFromRanges(input.File, cmd.Location())
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(secretsInArgOrEnvDetail(arg.Key, strings.ToUpper(command.Arg)))
		v = v.WithSuggestedFix(buildSecretsFix(meta.FixPriority))
		return &v
	}
	return nil
}

func (r *SecretsInArgOrEnvRule) checkEnvValues(
	input rules.LintInput,
	cmd *instructions.EnvCommand,
	meta rules.RuleMetadata,
) []rules.Violation {
	if cmd == nil {
		return nil
	}
	var violations []rules.Violation
	for _, kv := range cmd.Env {
		if !railsSecretEnvKeys[kv.Key] {
			continue
		}
		value := strings.TrimSpace(kv.Value)
		// Strip surrounding quotes for placeholder check.
		unquoted := value
		if len(unquoted) >= 2 &&
			((unquoted[0] == '"' && unquoted[len(unquoted)-1] == '"') ||
				(unquoted[0] == '\'' && unquoted[len(unquoted)-1] == '\'')) {
			unquoted = unquoted[1 : len(unquoted)-1]
		}
		if isPlaceholderSecretValue(unquoted) {
			continue
		}
		loc := rules.NewLocationFromRanges(input.File, cmd.Location())
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(secretsInArgOrEnvDetail(kv.Key, strings.ToUpper(command.Env)))
		v = v.WithSuggestedFix(buildSecretsFix(meta.FixPriority))
		violations = append(violations, v)
	}
	return violations
}

// isPlaceholderSecretValue reports whether a value is a known Rails
// placeholder constant (the dummy/build-time form Rails accepts).
func isPlaceholderSecretValue(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "":
		// Empty value (e.g. `ARG SECRET_KEY_BASE` with no default) is
		// flagged below — the user is passing the value via --build-arg
		// which would then leak into image cache key data. Don't treat
		// empty as a placeholder.
		return false
	case "1", "dummy", "secret_key_base_dummy":
		return true
	}
	// Bash-style placeholder patterns Rails accepts.
	return false
}

func secretsInArgOrEnvDetail(key, instruction string) string {
	return "Setting `" + key + "` via " + instruction + " bakes the value into the image's per-layer history " +
		"(`docker history --no-trunc <image>`) and into the build cache key data. Anyone who can pull the " +
		"image can recover the secret. Use BuildKit secret mounts " +
		"(`RUN --mount=type=secret,id=" + canonicalSecretID(key) + ",env=" + key + "`) for build-time " +
		"access; for asset compilation specifically, use `SECRET_KEY_BASE_DUMMY=1` so the build doesn't " +
		"need the real key at all (Rails 7.1+)."
}

// canonicalSecretID returns a conventional secret-mount ID for a given env var.
func canonicalSecretID(key string) string {
	return strings.ToLower(key)
}

// buildSecretsFix emits a non-edit suggestion. The fix can't auto-rewrite
// (the user's CI/secret-injection mechanism is repo-specific), but the
// description points at the supported alternative.
func buildSecretsFix(priority int) *rules.SuggestedFix {
	return &rules.SuggestedFix{
		Description: "Pass the secret via `RUN --mount=type=secret,id=...,env=...` instead of ARG/ENV. For " +
			"asset compilation use SECRET_KEY_BASE_DUMMY=1 (Rails 7.1+) to avoid needing the real key at " +
			"build time entirely.",
		Safety:      rules.FixUnsafe,
		Priority:    priority,
		IsPreferred: false,
	}
}

func init() {
	rules.Register(NewSecretsInArgOrEnvRule())
}
