package labels

import (
	"fmt"
	"slices"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NoDuplicateKeysRuleCode is the full rule code.
const NoDuplicateKeysRuleCode = rules.TallyRulePrefix + "labels/no-duplicate-keys"

// NoDuplicateKeysRule flags duplicate LABEL keys declared in the same stage.
type NoDuplicateKeysRule struct{}

// NewNoDuplicateKeysRule creates a new rule instance.
func NewNoDuplicateKeysRule() *NoDuplicateKeysRule {
	return &NoDuplicateKeysRule{}
}

// Metadata returns the rule metadata.
func (r *NoDuplicateKeysRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoDuplicateKeysRuleCode,
		Name:            "No duplicate label keys",
		Description:     "Detects Dockerfile LABEL keys that are set more than once in the same stage",
		DocURL:          rules.TallyDocURL(NoDuplicateKeysRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		IsExperimental:  false,
		FixPriority:     -1,
	}
}

// Check runs the rule.
func (r *NoDuplicateKeysRule) Check(input rules.LintInput) []rules.Violation {
	if input.Facts == nil {
		return nil
	}

	meta := r.Metadata()
	sm := input.SourceMap()
	escapeToken := labelEscapeToken(input)
	var violations []rules.Violation
	for _, stage := range input.Facts.Stages() {
		for _, group := range duplicateGroupsInOrder(stage) {
			if len(group) < 2 {
				continue
			}
			allEqual := allLabelValuesEqual(group)
			message := fmt.Sprintf("label key %q is overwritten later in this stage; Docker keeps the last value", group[0].Key)
			if allEqual {
				message = fmt.Sprintf("label key %q is repeated later with the same value in this stage", group[0].Key)
			}
			for _, duplicate := range group[:len(group)-1] {
				violation := rules.NewViolation(
					rules.NewLocationFromRanges(input.File, duplicate.Location),
					meta.Code,
					message,
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"Consolidate this key into a single LABEL pair so reviews do not need to infer which value wins.",
				)
				if fixes := buildDuplicateKeyFixes(input.File, sm, duplicate, meta, escapeToken); len(fixes) > 0 {
					violation = violation.WithSuggestedFixes(fixes)
				}
				violations = append(violations, violation)
			}
		}
	}
	return violations
}

func duplicateGroupsInOrder(stage *facts.StageFacts) [][]facts.LabelPairFact {
	if stage == nil {
		return nil
	}
	groupsByKey := stage.DuplicateLabelGroups()
	keys := make([]string, 0, len(groupsByKey))
	for key := range groupsByKey {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	groups := make([][]facts.LabelPairFact, 0, len(keys))
	for _, key := range keys {
		group := groupsByKey[key]
		slices.SortFunc(group, func(a, b facts.LabelPairFact) int {
			if a.CommandIndex != b.CommandIndex {
				return a.CommandIndex - b.CommandIndex
			}
			return a.PairIndex - b.PairIndex
		})
		groups = append(groups, group)
	}
	return groups
}

func allLabelValuesEqual(group []facts.LabelPairFact) bool {
	if len(group) < 2 {
		return true
	}
	first := group[0].Value
	for _, pair := range group[1:] {
		if pair.Value != first {
			return false
		}
	}
	return true
}

func buildDuplicateKeyFixes(
	file string,
	sm *sourcemap.SourceMap,
	pair facts.LabelPairFact,
	meta rules.RuleMetadata,
	escapeToken rune,
) []*rules.SuggestedFix {
	key := pair.Key
	return buildStandaloneLabelInstructionFixes(file, sm, pair, escapeToken, labelInstructionFixOptions{
		CommentDescription: fmt.Sprintf("Comment out duplicate LABEL %q (Docker keeps the last value)", key),
		DeleteDescription:  fmt.Sprintf("Delete duplicate LABEL %q", key),
		CommentPrefix:      fmt.Sprintf("# [commented out by tally - Docker keeps the last LABEL value for %s]: ", key),
		Safety:             rules.FixSafe,
		Priority:           meta.FixPriority,
	})
}

func init() {
	rules.Register(NewNoDuplicateKeysRule())
}
