package tally

import (
	"fmt"

	"github.com/wharflab/tally/internal/rules"
)

// PreferNginxSigquitRuleCode is the full rule code.
const PreferNginxSigquitRuleCode = rules.TallyRulePrefix + "prefer-nginx-sigquit"

// PreferNginxSigquitRule detects stages where PID 1 is clearly nginx or
// openresty but STOPSIGNAL is missing or not set to SIGQUIT.
//
// nginx treats SIGQUIT as graceful shutdown (workers finish in-flight
// requests) and SIGTERM as fast shutdown (active connections dropped).
// Containerized nginx images should therefore use SIGQUIT so that the
// runtime's stop timeout window is spent draining connections rather than
// killing them.
//
// Cross-rule interaction:
//
//   - tally/prefer-canonical-stopsignal handles QUIT → SIGQUIT. This rule
//     checks the normalized value, so non-canonical spellings that resolve
//     to SIGQUIT are treated as correct.
//   - tally/no-ungraceful-stopsignal may fire on the same STOPSIGNAL (e.g.
//     SIGKILL on an nginx stage). The fixer's category-based conflict
//     resolution lets no-ungraceful (category: correctness) win over this
//     rule (category: best-practice), so an ungraceful signal first becomes
//     SIGTERM. A subsequent --fix-unsafe pass then promotes SIGTERM to
//     SIGQUIT via this rule's wrong-signal fix.
type PreferNginxSigquitRule struct{}

// NewPreferNginxSigquitRule creates a new rule instance.
func NewPreferNginxSigquitRule() *PreferNginxSigquitRule {
	return &PreferNginxSigquitRule{}
}

// Metadata returns the rule metadata.
func (r *PreferNginxSigquitRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferNginxSigquitRuleCode,
		Name:            "Prefer SIGQUIT for nginx / openresty",
		Description:     "nginx and openresty containers should use STOPSIGNAL SIGQUIT for graceful shutdown",
		DocURL:          rules.TallyDocURL(PreferNginxSigquitRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "best-practice",
	}
}

// Check runs the prefer-nginx-sigquit rule.
//
// For each stage it determines whether PID 1 is nginx or openresty (exec
// form only — shell form and opaque wrappers hide the real PID 1). If so,
// it verifies that STOPSIGNAL is present and set to SIGQUIT.
//
// Windows stages are skipped because STOPSIGNAL has no effect on Windows
// containers.
func (r *PreferNginxSigquitRule) Check(input rules.LintInput) []rules.Violation {
	return checkDaemonStopsignal(input, daemonStopsignalRule{
		meta:         r.Metadata(),
		isDaemon:     isNginxOrOpenResty,
		targetSignal: signalSIGQUIT,
		wrongFix: daemonStopsignalFixSpec{
			Description: "Replace with SIGQUIT for graceful nginx shutdown",
			// Wrong-signal replacement is gated behind --fix-unsafe: the user
			// explicitly set a signal and SIGTERM is still a valid (if not
			// preferred) shutdown signal for nginx.
			Safety: rules.FixSuggestion,
		},
		missingFix: daemonStopsignalFixSpec{
			Description: "Add STOPSIGNAL SIGQUIT for graceful nginx shutdown",
			Safety:      rules.FixSafe,
		},
		wrongMessage: func(normalized string) string {
			return fmt.Sprintf("STOPSIGNAL %s should be SIGQUIT for nginx / openresty containers", normalized)
		},
		wrongDetail: "nginx treats SIGQUIT as graceful shutdown (workers drain" +
			" in-flight requests) and SIGTERM as fast shutdown (active" +
			" connections dropped)",
		missingMessage: "nginx / openresty container is missing STOPSIGNAL SIGQUIT",
		missingDetail: "Without STOPSIGNAL SIGQUIT, the container runtime sends SIGTERM" +
			" which triggers nginx's fast shutdown — active connections are" +
			" dropped instead of draining",
		insertText: "# [tally] SIGQUIT is the graceful shutdown signal for nginx\nSTOPSIGNAL SIGQUIT\n",
	})
}

func init() {
	rules.Register(NewPreferNginxSigquitRule())
}
