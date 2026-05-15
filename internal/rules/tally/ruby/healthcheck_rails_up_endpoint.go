package ruby

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/stagename"
)

// HealthcheckRailsUpEndpointRuleCode is the full rule code.
const HealthcheckRailsUpEndpointRuleCode = rules.TallyRulePrefix + "ruby/healthcheck-rails-up-endpoint"

// healthcheckRailsUpEndpointFixPriority keeps this rule's edits ordered alongside
// the other Ruby rules.
const healthcheckRailsUpEndpointFixPriority = 88

// HealthcheckRailsUpEndpointRule flags Rails 7.1+ runtime stages without
// a HEALTHCHECK against `/up`, and HEALTHCHECKs that use curl/wget when
// Ruby's stdlib Net::HTTP would do the job without an extra apt
// install.
type HealthcheckRailsUpEndpointRule struct{}

// NewHealthcheckRailsUpEndpointRule creates the rule.
func NewHealthcheckRailsUpEndpointRule() *HealthcheckRailsUpEndpointRule {
	return &HealthcheckRailsUpEndpointRule{}
}

// Metadata returns the rule metadata.
func (r *HealthcheckRailsUpEndpointRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            HealthcheckRailsUpEndpointRuleCode,
		Name:            "Use Rails 7.1+ /up healthcheck via Ruby stdlib Net::HTTP",
		Description:     "Rails runtime image lacks HEALTHCHECK or uses curl/wget instead of Ruby stdlib Net::HTTP",
		DocURL:          rules.TallyDocURL(HealthcheckRailsUpEndpointRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "correctness",
		FixPriority:     healthcheckRailsUpEndpointFixPriority,
	}
}

// Check runs the rule.
func (r *HealthcheckRailsUpEndpointRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	finalIdx := input.FinalStageIndex()
	if finalIdx < 0 || finalIdx >= len(input.Stages) {
		return nil
	}
	stage := input.Stages[finalIdx]
	sf := input.Facts.Stage(finalIdx)
	if sf == nil {
		return nil
	}
	if sf.BaseImageOS == semantic.BaseImageOSWindows {
		return nil
	}
	if stagename.LooksLikeDev(stage.Name) {
		return nil
	}
	if !stageLooksLikeRuby(input.Semantic, finalIdx, stage, sf) {
		return nil
	}
	if !stageLooksLikeRubyWebServerWithParents(input, finalIdx, stage, sf) {
		// Workers (sidekiq/resque) don't expose an HTTP endpoint; the
		// `/up` healthcheck recommendation only applies to Rails web
		// servers. The walk also follows inherited CMD/ENTRYPOINT when
		// the final stage builds on a named parent stage.
		return nil
	}

	healthCheck := findLastHealthCheck(stage)
	if healthCheck == nil {
		// HEALTHCHECK can be inherited from an earlier stage that this
		// final stage `FROM`s. Walk the parent chain inside the same
		// Dockerfile to see if a parent stage already has one.
		// (Inheritance from the upstream image content can't be checked
		// from the Dockerfile alone, but the official Ruby base images
		// don't ship with a HEALTHCHECK, and this rule is gated on
		// Ruby-shaped final stages — so this remains a useful trigger
		// in practice.)
		if priorStageHasHealthCheck(input, stage) {
			return nil
		}
		// Variant 1: no HEALTHCHECK at all on a Rails-app runtime stage.
		loc := finalStageFromLocation(input, finalIdx)
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(missingHealthCheckDetail()).
			WithSuggestedFix(buildMissingHealthCheckFix(meta.FixPriority))
		return []rules.Violation{v}
	}

	// Variant 2: HEALTHCHECK is NONE — explicit opt-out, suppress.
	if healthCheck.Health == nil || len(healthCheck.Health.Test) == 0 {
		return nil
	}
	if strings.EqualFold(healthCheck.Health.Test[0], "NONE") {
		return nil
	}

	// Variant 3: HEALTHCHECK uses curl/wget. Suggest the Ruby-native form.
	if healthCheckUsesCurlOrWget(healthCheck) {
		loc := rules.NewLocationFromRanges(input.File, healthCheck.Location())
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(curlBasedHealthCheckDetail()).
			WithSuggestedFix(buildCurlHealthCheckRewriteFix(meta.FixPriority))
		return []rules.Violation{v}
	}

	return nil
}

func missingHealthCheckDetail() string {
	return "Ruby web server runtime image has no HEALTHCHECK. Rails 7.1+ mounts `Rails::HealthController` " +
		"at `/up` by default; if the app is Rails 7.1+, probe `/up`, otherwise probe whatever endpoint the " +
		"app exposes (substitute the path below). The Ruby stdlib's Net::HTTP is already in the image, so " +
		"this is the cheapest probe — no extra apt install for `curl`. Add a HEALTHCHECK that calls the " +
		"endpoint via `ruby -rnet/http -e ...`."
}

func curlBasedHealthCheckDetail() string {
	return "Healthcheck uses `curl`/`wget`, which on `ruby:*-slim`/`ruby:*-alpine` bases requires an extra " +
		"`apt-get install curl` step — ~3 MiB to add a tool that's already in the image as the Ruby " +
		"stdlib's Net::HTTP. Switch to the Ruby-native form to drop the dependency."
}

// rubyWebServerCommands is the subset of long-running Ruby servers that
// terminate HTTP — the only ones for which a `/up` HTTP healthcheck
// makes sense. Workers (`sidekiq`, `resque`) listen on a queue, not a
// port, so probing them for HTTP would surface a permanently failing
// healthcheck.
//
// `rails` is special: bare `rails` or `rails server` is a web server,
// but `rails db:migrate` / `rails runner` / `rails console` are
// one-shot jobs. See `argvIncludesRubyWebServer` for the filter.
var rubyWebServerCommands = map[string]bool{
	"puma":      true,
	"unicorn":   true,
	"thrust":    true,
	"rackup":    true,
	"falcon":    true,
	"thin":      true,
	"passenger": true,
	"iodine":    true,
}

// stageLooksLikeRubyWebServerWithParents reports whether the stage
// runs a Ruby HTTP server, following inherited CMD/ENTRYPOINT when the
// stage uses `FROM <named-stage>`. Docker inherits CMD and ENTRYPOINT
// independently: if the final stage sets only CMD, the ENTRYPOINT
// comes from the parent; if it sets only ENTRYPOINT, CMD is reset.
// The walk resolves the effective argv for whichever instruction the
// stage didn't override.
func stageLooksLikeRubyWebServerWithParents(
	input rules.LintInput, finalIdx int, stage instructions.Stage, sf *facts.StageFacts,
) bool {
	entry, cmd := resolveEffectiveEntrypointAndCmd(input, stage)
	if entry != nil && argvIncludesRubyWebServer(entry.CmdLine) {
		return true
	}
	if cmd != nil && argvIncludesRubyWebServer(cmd.CmdLine) {
		return true
	}
	_ = sf
	_ = finalIdx
	return false
}

// resolveEffectiveEntrypointAndCmd returns the effective
// ENTRYPOINT/CMD argv for stage by walking the parent stage chain
// inside the same Dockerfile. CMD and ENTRYPOINT are resolved
// independently because Docker inherits them as separate slots.
//
// Rules per Docker semantics:
//   - If the local stage sets ENTRYPOINT, that's the effective value
//     and the local CMD is used (or empty if not set).
//   - If the local stage sets only CMD, ENTRYPOINT is inherited.
//   - If the local stage sets neither, both are inherited.
func resolveEffectiveEntrypointAndCmd(
	input rules.LintInput, stage instructions.Stage,
) (*instructions.EntrypointCommand, *instructions.CmdCommand) {
	entry, cmd := lastEntrypointAndCmd(stage)
	if entry != nil && cmd != nil {
		return entry, cmd
	}
	// One or both missing — walk parent chain for the missing slot(s).
	stagesByName := make(map[string]instructions.Stage, len(input.Stages))
	for _, s := range input.Stages {
		if s.Name != "" {
			stagesByName[strings.ToLower(s.Name)] = s
		}
	}
	parent := stage.BaseName
	for range input.Stages {
		parentStage, ok := stagesByName[strings.ToLower(parent)]
		if !ok {
			return entry, cmd
		}
		parentEntry, parentCmd := lastEntrypointAndCmd(parentStage)
		if entry == nil && parentEntry != nil {
			entry = parentEntry
		}
		if cmd == nil && parentCmd != nil {
			cmd = parentCmd
		}
		if entry != nil && cmd != nil {
			return entry, cmd
		}
		parent = parentStage.BaseName
	}
	return entry, cmd
}

// lastEntrypointAndCmd returns the final ENTRYPOINT and CMD instructions
// declared in stage (Docker uses the last of each).
func lastEntrypointAndCmd(stage instructions.Stage) (*instructions.EntrypointCommand, *instructions.CmdCommand) {
	var lastEntrypoint *instructions.EntrypointCommand
	var lastCmd *instructions.CmdCommand
	for _, c := range stage.Commands {
		switch cc := c.(type) {
		case *instructions.EntrypointCommand:
			lastEntrypoint = cc
		case *instructions.CmdCommand:
			lastCmd = cc
		}
	}
	return lastEntrypoint, lastCmd
}

// argvIncludesRubyWebServer reports whether any argv token's basename
// matches a Ruby HTTP server name.
//
// `rails` requires special handling: `rails server` (or bare `rails`,
// which prints help and exits, so unlikely as final CMD) are
// web-serving commands, but `rails db:migrate`, `rails runner`,
// `rails console`, etc. are one-shot jobs that don't expose HTTP.
func argvIncludesRubyWebServer(argv []string) bool {
	// Build a flat list of words for sequential look-ahead at `rails`
	// matches.
	var words []string
	for _, token := range argv {
		for word := range strings.FieldsSeq(token) {
			words = append(words, word)
		}
	}
	for i, word := range words {
		basename := strings.ToLower(commandBasename(word))
		if rubyWebServerCommands[basename] {
			return true
		}
		if basename == railsServerCommand {
			// Treat `rails` as a web server only when followed by
			// `server` (or `s`, the canonical alias). Anything else —
			// `db:migrate`, `runner`, `console`, etc. — is a one-shot
			// job and shouldn't get the `/up` healthcheck advice.
			if i+1 < len(words) {
				next := strings.ToLower(words[i+1])
				if next == "server" || next == "s" {
					return true
				}
			}
		}
	}
	return false
}

// findLastHealthCheck returns the last HEALTHCHECK instruction in the
// stage (HEALTHCHECK is overridden by later instances).
func findLastHealthCheck(stage instructions.Stage) *instructions.HealthCheckCommand {
	var last *instructions.HealthCheckCommand
	for _, cmd := range stage.Commands {
		if hc, ok := cmd.(*instructions.HealthCheckCommand); ok {
			last = hc
		}
	}
	return last
}

// priorStageHasHealthCheck reports whether stage's parent (`FROM
// <name>` resolves to another stage in the same Dockerfile) — or any
// ancestor reachable via the `FROM <stage>` chain — declares a
// HEALTHCHECK that this stage would inherit. The walk is bounded by
// the number of stages and stops at the first stage whose `From` is
// an external image (not a stage name in the same Dockerfile).
func priorStageHasHealthCheck(input rules.LintInput, stage instructions.Stage) bool {
	parent := stage.BaseName
	if parent == "" {
		return false
	}
	stagesByName := make(map[string]instructions.Stage, len(input.Stages))
	for _, s := range input.Stages {
		if s.Name != "" {
			stagesByName[strings.ToLower(s.Name)] = s
		}
	}
	for range input.Stages {
		parentStage, ok := stagesByName[strings.ToLower(parent)]
		if !ok {
			return false
		}
		if findLastHealthCheck(parentStage) != nil {
			return true
		}
		parent = parentStage.BaseName
	}
	return false
}

// healthCheckUsesCurlOrWget reports whether the HEALTHCHECK CMD's
// argv contains `curl` or `wget` as the first executable.
func healthCheckUsesCurlOrWget(hc *instructions.HealthCheckCommand) bool {
	if hc == nil || hc.Health == nil {
		return false
	}
	test := hc.Health.Test
	if len(test) == 0 {
		return false
	}
	// Test slice's first element identifies the form: CMD or CMD-SHELL.
	// The remaining elements are the command argv.
	for i := 1; i < len(test); i++ {
		token := test[i]
		// Split on whitespace for shell-form invocations.
		for word := range strings.FieldsSeq(token) {
			if word == "curl" || word == "wget" ||
				strings.HasSuffix(word, "/curl") || strings.HasSuffix(word, "/wget") {
				return true
			}
			// Stop at the first non-flag, non-curl/wget token — that's
			// the command, not curl.
			if !strings.HasPrefix(word, "-") {
				return false
			}
		}
	}
	return false
}

// buildMissingHealthCheckFix proposes adding the canonical Ruby-native
// HEALTHCHECK as a non-edit FixSuggestion.
func buildMissingHealthCheckFix(priority int) *rules.SuggestedFix {
	return &rules.SuggestedFix{
		Description: "Add a Ruby-native HEALTHCHECK against /up: " +
			"HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 " +
			`CMD ["ruby", "-rnet/http", "-e", ` +
			`"exit Net::HTTP.get_response(URI('http://127.0.0.1:3000/up')).is_a?(Net::HTTPSuccess) ? 0 : 1"]. ` +
			"Net::HTTP is in the Ruby stdlib so no extra packages are needed.",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: false,
	}
}

// buildCurlHealthCheckRewriteFix proposes replacing the curl/wget form
// with the Ruby-native equivalent. Non-edit.
func buildCurlHealthCheckRewriteFix(priority int) *rules.SuggestedFix {
	return &rules.SuggestedFix{
		Description: "Replace the curl/wget HEALTHCHECK with the Ruby-native form (drops the apt-get " +
			`install): CMD ["ruby", "-rnet/http", "-e", ` +
			`"exit Net::HTTP.get_response(URI('http://127.0.0.1:3000/up')).is_a?(Net::HTTPSuccess) ? 0 : 1"]`,
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		IsPreferred: false,
	}
}

func init() {
	rules.Register(NewHealthcheckRailsUpEndpointRule())
}
