package tally

import (
	"fmt"

	"github.com/wharflab/tally/internal/rules"
)

// PreferSystemdSigrtminPlus3RuleCode is the full rule code.
const PreferSystemdSigrtminPlus3RuleCode = rules.TallyRulePrefix + "prefer-systemd-sigrtmin-plus-3"

// PreferSystemdSigrtminPlus3Rule detects stages where PID 1 is clearly
// systemd/init but STOPSIGNAL is missing or not set to SIGRTMIN+3.
//
// systemd requires SIGRTMIN+3 to trigger a clean manager shutdown (analogous
// to "systemctl halt"). Without it the container runtime sends SIGTERM (signal
// 15), which systemd interprets as an isolate-to-rescue-mode request — not a
// clean shutdown.
//
// Cross-rule interaction:
//
//   - tally/prefer-canonical-stopsignal handles RTMIN+3 → SIGRTMIN+3. This
//     rule checks the normalized value, so RTMIN+3 is treated as correct.
//   - tally/no-ungraceful-stopsignal may fire on the same STOPSIGNAL (e.g.
//     SIGKILL on a systemd stage). This rule's fix uses Priority -1 to claim
//     the signal edit range first, replacing with SIGRTMIN+3 instead of
//     SIGTERM.
type PreferSystemdSigrtminPlus3Rule struct{}

// NewPreferSystemdSigrtminPlus3Rule creates a new rule instance.
func NewPreferSystemdSigrtminPlus3Rule() *PreferSystemdSigrtminPlus3Rule {
	return &PreferSystemdSigrtminPlus3Rule{}
}

// Metadata returns the rule metadata.
func (r *PreferSystemdSigrtminPlus3Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferSystemdSigrtminPlus3RuleCode,
		Name:            "Prefer SIGRTMIN+3 for systemd/init",
		Description:     "systemd/init containers should use STOPSIGNAL SIGRTMIN+3 for clean shutdown",
		DocURL:          rules.TallyDocURL(PreferSystemdSigrtminPlus3RuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the prefer-systemd-sigrtmin-plus-3 rule.
//
// For each stage it determines whether PID 1 is a systemd/init binary (exec
// form only — shell form hides the real PID 1). If so, it verifies that
// STOPSIGNAL is present and set to SIGRTMIN+3.
//
// Windows stages are skipped because STOPSIGNAL has no effect on Windows
// containers.
func (r *PreferSystemdSigrtminPlus3Rule) Check(input rules.LintInput) []rules.Violation {
	return checkDaemonStopsignal(input, daemonStopsignalRule{
		meta:         r.Metadata(),
		isDaemon:     isSystemdInit,
		targetSignal: signalSIGRTMINPlus3,
		wrongFix: daemonStopsignalFixSpec{
			Description: "Replace with SIGRTMIN+3 for clean systemd shutdown",
			Safety:      rules.FixSafe,
		},
		missingFix: daemonStopsignalFixSpec{
			Description: "Add STOPSIGNAL SIGRTMIN+3 for clean systemd shutdown",
			Safety:      rules.FixSafe,
		},
		wrongMessage: func(normalized string) string {
			return fmt.Sprintf("STOPSIGNAL %s should be SIGRTMIN+3 for systemd/init containers", normalized)
		},
		wrongDetail: "systemd requires SIGRTMIN+3 to trigger a clean manager" +
			" shutdown, analogous to systemctl halt",
		missingMessage: "systemd/init container is missing STOPSIGNAL SIGRTMIN+3",
		missingDetail: "Without STOPSIGNAL SIGRTMIN+3, the container runtime sends" +
			" SIGTERM which systemd interprets as an isolate-to-rescue-mode" +
			" request, not a clean shutdown",
		insertText: "# [tally] SIGRTMIN+3 is the graceful shutdown signal for systemd/init\nSTOPSIGNAL SIGRTMIN+3\n",
	})
}

func init() {
	rules.Register(NewPreferSystemdSigrtminPlus3Rule())
}
