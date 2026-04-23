package tally

import (
	"fmt"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// UserExplicitGroupDropsSupplementaryGroupsRuleCode is the full rule code.
const UserExplicitGroupDropsSupplementaryGroupsRuleCode = rules.TallyRulePrefix +
	"user-explicit-group-drops-supplementary-groups"

// UserExplicitGroupDropsSupplementaryGroupsRule detects `USER name:group`
// instructions that silently drop supplementary groups established earlier in
// the Dockerfile via useradd -G / adduser / addgroup / usermod -aG / gpasswd
// on Linux, or net localgroup /add / Add-LocalGroupMember on Windows.
//
// Docker's USER documentation is explicit:
//
//	"Note that when specifying a group for the user, the user will have only
//	the specified group membership. Any other configured group memberships
//	will be ignored."
//
// This applies to both Linux and Windows containers. The fix drops the
// ":group" portion so Docker honors the user's supplementary group set.
//
// Cross-rule interaction:
//
//   - tally/named-identity-in-passwdless-stage: shares the USER operand span.
//     Resolved by skipping passwd-less (scratch-rooted) stages in this rule —
//     those are named-identity's territory.
//   - tally/user-created-but-never-used: fires only when final effective user
//     is root. Our rule requires an explicit USER name:group (non-root).
//     Complementary.
//   - tally/copy-after-user-without-chown: edits COPY/ADD. No overlap.
type UserExplicitGroupDropsSupplementaryGroupsRule struct{}

// NewUserExplicitGroupDropsSupplementaryGroupsRule creates a new rule instance.
func NewUserExplicitGroupDropsSupplementaryGroupsRule() *UserExplicitGroupDropsSupplementaryGroupsRule {
	return &UserExplicitGroupDropsSupplementaryGroupsRule{}
}

// Metadata returns the rule metadata.
func (r *UserExplicitGroupDropsSupplementaryGroupsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code: UserExplicitGroupDropsSupplementaryGroupsRuleCode,
		Name: "USER explicit group drops supplementary groups",
		Description: "USER name:group drops supplementary groups the Dockerfile " +
			"established via useradd -G / usermod / gpasswd / net localgroup / Add-LocalGroupMember",
		DocURL:          rules.TallyDocURL(UserExplicitGroupDropsSupplementaryGroupsRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
	}
}

// Check runs the rule across all stages.
func (r *UserExplicitGroupDropsSupplementaryGroupsRule) Check(input rules.LintInput) []rules.Violation {
	sem := input.Semantic
	if sem == nil {
		return nil
	}
	fileFacts := input.Facts
	if fileFacts == nil {
		return nil
	}

	meta := r.Metadata()
	sm := input.SourceMap()

	var violations []rules.Violation

	for stageIdx := range sem.StageCount() {
		sf := fileFacts.Stage(stageIdx)
		if sf == nil || len(sf.UserCommands) == 0 {
			continue
		}

		info := sem.StageInfo(stageIdx)
		isWindows := info != nil && info.IsWindows()

		// Defer to tally/named-identity-in-passwdless-stage for passwd-less
		// Linux stages — it owns the USER operand span there and would rewrite
		// to a numeric UID, which is a different intent from removing :group.
		if !isWindows {
			if _, _, passwdless := inheritedIdentityDBState(sem, fileFacts, stageIdx); passwdless {
				continue
			}
		}

		suppGroups := collectStageSupplementaryGroups(input, fileFacts, stageIdx, isWindows)
		if len(suppGroups) == 0 {
			continue
		}

		for _, userCmd := range sf.UserCommands {
			if v := r.checkUserCmd(userCmd, stageIdx, suppGroups, isWindows, input.File, sm, meta); v != nil {
				violations = append(violations, *v)
			}
		}
	}

	return violations
}

// checkUserCmd evaluates a single USER instruction and builds a violation if
// it specifies an explicit group whose user has recorded supplementary groups.
func (r *UserExplicitGroupDropsSupplementaryGroupsRule) checkUserCmd(
	userCmd *instructions.UserCommand,
	stageIdx int,
	suppGroups map[string][]string,
	isWindows bool,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.Violation {
	user, group := splitUserGroup(userCmd.User)
	if group == "" || user == "" {
		return nil
	}
	if facts.IsRootUser(user) {
		return nil
	}
	if isNumericUser(user) {
		return nil
	}

	key := user
	if isWindows {
		key = strings.ToLower(user)
	}
	groups, ok := suppGroups[key]
	if !ok || len(groups) == 0 {
		return nil
	}

	msg := fmt.Sprintf(
		"USER %q specifies explicit group %q but this user has supplementary groups (%s) that Docker will ignore",
		user, group, strings.Join(groups, ", "),
	)

	detail := "Docker's USER name:group sets ONLY the given group at runtime. Supplementary groups added earlier — " +
		"via useradd -G / adduser / addgroup / usermod -aG / gpasswd on Linux, or net localgroup /add / " +
		"Add-LocalGroupMember on Windows — are silently dropped. Drop the \":group\" portion to preserve " +
		"supplementary group membership, or accept the explicit group as an intentional primary-group override."

	loc := rules.NewLocationFromRanges(file, userCmd.Location())
	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(detail)
	v.StageIndex = stageIdx

	if fix := r.buildFix(userCmd, user, group, file, sm); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

// buildFix removes the ":group" portion of USER name:group, preserving
// indentation, keyword casing, and any surrounding whitespace. The edit range
// is narrow (operand only), so formatting rules compose cleanly.
//
// Safety is FixSuggestion because the explicit group may have been an
// intentional primary-group override.
func (r *UserExplicitGroupDropsSupplementaryGroupsRule) buildFix(
	userCmd *instructions.UserCommand,
	user, group, file string,
	sm *sourcemap.SourceMap,
) *rules.SuggestedFix {
	locs := userCmd.Location()
	if len(locs) == 0 || sm == nil {
		return nil
	}

	originalOperand := userCmd.User
	if originalOperand == "" {
		return nil
	}

	startLine := locs[0].Start.Line
	endLine := sm.ResolveEndLine(locs[0].End.Line)

	// Scan the instruction's full physical-line span so continuation forms
	// (USER \ on one line, operand on the next) land the edit on the correct
	// physical line.
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		srcLine := sm.Line(lineNum - 1) // SourceMap is 0-based
		idx := strings.Index(srcLine, originalOperand)
		if idx < 0 {
			continue
		}

		return &rules.SuggestedFix{
			Description: fmt.Sprintf(
				"Drop %q from USER (Docker would otherwise drop supplementary groups)",
				":"+group,
			),
			Safety: rules.FixSuggestion,
			Edits: []rules.TextEdit{{
				Location: rules.NewRangeLocation(file, lineNum, idx, lineNum, idx+len(originalOperand)),
				NewText:  user,
			}},
		}
	}

	return nil
}

// collectStageSupplementaryGroups walks the given stage and its FROM ancestry
// chain, returning a map of username → sorted-unique supplementary groups.
// When isWindows is true, usernames are lower-cased to make matching
// case-insensitive (Windows account names are case-insensitive).
func collectStageSupplementaryGroups(
	input rules.LintInput,
	fileFacts *facts.FileFacts,
	stageIdx int,
	isWindows bool,
) map[string][]string {
	sets := map[string]map[string]bool{}

	visitStageAndAncestryRunScripts(input, fileFacts, stageIdx, func(sv scriptVisit) {
		for _, m := range findUserMembershipCmds(sv.Script, sv.Variant) {
			if m.User == "" || len(m.Groups) == 0 {
				continue
			}
			key := m.User
			if isWindows {
				key = strings.ToLower(key)
			}
			set, ok := sets[key]
			if !ok {
				set = map[string]bool{}
				sets[key] = set
			}
			for _, g := range m.Groups {
				g = strings.TrimSpace(g)
				if g != "" {
					set[g] = true
				}
			}
		}
	})

	if len(sets) == 0 {
		return nil
	}

	out := make(map[string][]string, len(sets))
	for user, set := range sets {
		groups := make([]string, 0, len(set))
		for g := range set {
			groups = append(groups, g)
		}
		slices.Sort(groups)
		out[user] = groups
	}
	return out
}

func init() {
	rules.Register(NewUserExplicitGroupDropsSupplementaryGroupsRule())
}
