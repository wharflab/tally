package tally

import (
	"fmt"
	"strings"

	"github.com/wharflab/tally/internal/rules"
)

// PreferCanonicalStopsignalRuleCode is the full rule code.
const PreferCanonicalStopsignalRuleCode = rules.TallyRulePrefix + "prefer-canonical-stopsignal"

// PreferCanonicalStopsignalRule detects STOPSIGNAL tokens that use non-canonical
// spelling — quoted values, missing SIG prefix, lowercase, numeric aliases, or
// non-standard RT signal names — and suggests the canonical form.
type PreferCanonicalStopsignalRule struct{}

// NewPreferCanonicalStopsignalRule creates a new rule instance.
func NewPreferCanonicalStopsignalRule() *PreferCanonicalStopsignalRule {
	return &PreferCanonicalStopsignalRule{}
}

// Metadata returns the rule metadata.
func (r *PreferCanonicalStopsignalRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferCanonicalStopsignalRuleCode,
		Name:            "Prefer Canonical STOPSIGNAL",
		Description:     "STOPSIGNAL should use canonical signal names (e.g. SIGTERM, not TERM or 15)",
		DocURL:          rules.TallyDocURL(PreferCanonicalStopsignalRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "style",
	}
}

// Check runs the prefer-canonical-stopsignal rule.
//
// Windows stages are skipped because STOPSIGNAL has no effect on Windows
// containers — canonicalizing a meaningless token is not useful.
func (r *PreferCanonicalStopsignalRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	var violations []rules.Violation

	visitStopsignals(input, func(v stopsignalVisit) {
		// Already canonical — nothing to do.
		if strings.TrimSpace(v.raw) == v.normalized {
			return
		}

		loc := rules.NewLocationFromRanges(input.File, v.cmd.Location())

		msg := fmt.Sprintf(
			"STOPSIGNAL %s should be written as %s",
			strings.TrimSpace(v.raw), v.normalized,
		)

		violation := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(fmt.Sprintf("Use the canonical signal name %s for clarity and consistency", v.normalized))

		if editLoc := signalEditLocation(input.File, input.Source, v.cmd); editLoc != nil {
			violation = violation.WithSuggestedFix(&rules.SuggestedFix{
				Description: "Replace with canonical signal name " + v.normalized,
				Safety:      rules.FixSafe,
				Priority:    meta.FixPriority,
				Edits: []rules.TextEdit{
					{Location: *editLoc, NewText: v.normalized},
				},
			})
		}

		violations = append(violations, violation)
	})

	return violations
}

func init() {
	rules.Register(NewPreferCanonicalStopsignalRule())
}
