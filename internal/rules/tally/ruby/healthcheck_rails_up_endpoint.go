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
	if !stageLooksLikeLongRunningRubyServer(stage, sf) {
		return nil
	}

	healthCheck := findLastHealthCheck(stage)
	if healthCheck == nil {
		// Variant 1: no HEALTHCHECK at all on a Rails-app runtime stage.
		loc := finalStageFromLocation(input, finalIdx)
		v := rules.NewViolation(loc, meta.Code, meta.Description, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(missingHealthCheckDetail()).
			WithSuggestedFix(buildMissingHealthCheckFix(meta.FixPriority))
		return []rules.Violation{v}
	}

	// Variant 2: HEALTHCHECK is NONE — explicit opt-out, suppress.
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
	return "Rails 7.1+ mounts `Rails::HealthController` at `/up` by default. The Ruby stdlib's Net::HTTP " +
		"(already in the image) is the cheapest way to probe it — no extra apt install for `curl`. " +
		"Add a HEALTHCHECK that calls `/up` via `ruby -rnet/http -e ...`."
}

func curlBasedHealthCheckDetail() string {
	return "Healthcheck uses `curl`/`wget`, which on `ruby:*-slim`/`ruby:*-alpine` bases requires an extra " +
		"`apt-get install curl` step — ~3 MiB to add a tool that's already in the image as the Ruby " +
		"stdlib's Net::HTTP. Switch to the Ruby-native form to drop the dependency."
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

// Compile-time assertion that we're consuming a stable CopyCommand
// reference (used by stageLooksLikeRailsApp).
var _ = (*facts.StageFacts)(nil)

func init() {
	rules.Register(NewHealthcheckRailsUpEndpointRule())
}
