package tally

import (
	"fmt"

	"github.com/wharflab/tally/internal/rules"
)

// NoUngracefulStopsignalRuleCode is the full rule code.
const NoUngracefulStopsignalRuleCode = rules.TallyRulePrefix + "no-ungraceful-stopsignal"

// ungracefulSignals maps normalized signal names that defeat the purpose of STOPSIGNAL.
var ungracefulSignals = map[string]string{
	"SIGKILL": "cannot be caught or ignored; the container gets no chance to clean up",
	"SIGSTOP": "suspends the process instead of stopping it; the container will not terminate",
}

// NoUngracefulStopsignalRule detects STOPSIGNAL values that defeat the purpose
// of graceful container shutdown, specifically SIGKILL and SIGSTOP.
type NoUngracefulStopsignalRule struct{}

// NewNoUngracefulStopsignalRule creates a new rule instance.
func NewNoUngracefulStopsignalRule() *NoUngracefulStopsignalRule {
	return &NoUngracefulStopsignalRule{}
}

// Metadata returns the rule metadata.
func (r *NoUngracefulStopsignalRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoUngracefulStopsignalRuleCode,
		Name:            "No Ungraceful STOPSIGNAL",
		Description:     "STOPSIGNAL should not use signals that prevent graceful shutdown",
		DocURL:          rules.TallyDocURL(NoUngracefulStopsignalRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// Check runs the no-ungraceful-stopsignal rule.
//
// Windows stages are skipped because STOPSIGNAL has no effect on Windows
// containers (POSIX signals are not delivered). Reporting a signal as
// "ungraceful" on a platform where it does nothing would be misleading.
func (r *NoUngracefulStopsignalRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	var violations []rules.Violation

	visitStopsignals(input, func(v stopsignalVisit) {
		reason, isUngraceful := ungracefulSignals[v.normalized]
		if !isUngraceful {
			return
		}

		loc := rules.NewLocationFromRanges(input.File, v.cmd.Location())

		msg := fmt.Sprintf(
			"STOPSIGNAL %s is not a graceful stop signal: %s",
			v.normalized, reason,
		)

		violation := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail("Replace with a signal that allows graceful shutdown (e.g. SIGTERM)")

		if editLoc := signalEditLocation(input.File, input.Source, v.cmd); editLoc != nil {
			violation = violation.WithSuggestedFix(&rules.SuggestedFix{
				Description: "Replace with SIGTERM for graceful shutdown",
				Safety:      rules.FixSuggestion,
				Edits: []rules.TextEdit{
					{Location: *editLoc, NewText: "SIGTERM"},
				},
				IsPreferred: true,
			})
		}

		violations = append(violations, violation)
	})

	return violations
}

func init() {
	rules.Register(NewNoUngracefulStopsignalRule())
}
