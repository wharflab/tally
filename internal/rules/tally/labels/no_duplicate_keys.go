package labels

import (
	"fmt"
	"slices"
	"strings"

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

func labelEscapeToken(input rules.LintInput) rune {
	if input.AST == nil {
		return '\\'
	}
	return input.AST.EscapeToken
}

func buildDuplicateKeyFixes(
	file string,
	sm *sourcemap.SourceMap,
	pair facts.LabelPairFact,
	meta rules.RuleMetadata,
	escapeToken rune,
) []*rules.SuggestedFix {
	if sm == nil || pair.Command == nil || len(pair.Command.Labels) != 1 {
		return nil
	}
	locs := pair.Command.Location()
	if len(locs) == 0 {
		return nil
	}

	startLine := locs[0].Start.Line
	endLine := sm.ResolveEndLineWithEscape(locs[0].End.Line, escapeToken)
	if startLine <= 0 || endLine < startLine || endLine > sm.LineCount() {
		return nil
	}

	lastLine := sm.Line(endLine - 1)
	editLoc := rules.NewRangeLocation(file, startLine, 0, endLine, len(lastLine))
	deleteLoc := deleteInstructionLocation(file, sm, startLine, endLine)
	key := pair.Key
	commentedText := commentOutLabelInstruction(sm, startLine, endLine, key)

	return []*rules.SuggestedFix{
		{
			Description: fmt.Sprintf("Comment out duplicate LABEL %q (Docker keeps the last value)", key),
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			IsPreferred: true,
			Edits:       []rules.TextEdit{{Location: editLoc, NewText: commentedText}},
		},
		{
			Description: fmt.Sprintf("Delete duplicate LABEL %q", key),
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits:       []rules.TextEdit{{Location: deleteLoc, NewText: ""}},
		},
	}
}

func commentOutLabelInstruction(sm *sourcemap.SourceMap, startLine, endLine int, key string) string {
	lines := make([]string, 0, endLine-startLine+1)
	prefix := fmt.Sprintf("# [commented out by tally - Docker keeps the last LABEL value for %s]: ", key)
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		line := sm.Line(lineNum - 1)
		if lineNum == startLine {
			lines = append(lines, prefix+line)
			continue
		}
		lines = append(lines, "# "+line)
	}
	return strings.Join(lines, "\n")
}

func deleteInstructionLocation(file string, sm *sourcemap.SourceMap, startLine, endLine int) rules.Location {
	lastLine := sm.Line(endLine - 1)
	if endLine < sm.LineCount() {
		return rules.NewRangeLocation(file, startLine, 0, endLine+1, 0)
	}
	return rules.NewRangeLocation(file, startLine, 0, endLine, len(lastLine))
}

func init() {
	rules.Register(NewNoDuplicateKeysRule())
}
