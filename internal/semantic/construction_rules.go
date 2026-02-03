package semantic

import "github.com/tinovyatkin/tally/internal/rules"

// ConstructionRuleCodes returns rule codes that can be emitted during semantic model construction.
//
// These checks run outside the per-rule registry and are later processed by the same
// enable/disable and severity override pipeline as normal violations.
func ConstructionRuleCodes() []string {
	return []string{
		rules.HadolintRulePrefix + "DL3012",
		rules.HadolintRulePrefix + "DL3023",
		rules.HadolintRulePrefix + "DL3043",
		rules.HadolintRulePrefix + "DL3061",
	}
}
