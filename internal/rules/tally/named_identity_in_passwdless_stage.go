package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NamedIdentityInPasswdlessStageRuleCode is the full rule code.
const NamedIdentityInPasswdlessStageRuleCode = rules.TallyRulePrefix + "named-identity-in-passwdless-stage"

// NamedIdentityInPasswdlessStageRule detects named (non-numeric) user or group
// references in USER instructions or COPY/ADD --chown flags within stages that
// lack /etc/passwd or /etc/group. Named identity resolution requires these
// database files; without them, the build or runtime will fail.
//
// This is a common pitfall in scratch and multi-stage builds that inherit from
// scratch without copying the passwd/group databases from a builder stage.
//
// Cross-rule interaction:
//
//   - tally/shell-run-in-scratch also targets scratch stages but checks for
//     shell availability, not identity resolution. Complementary; no suppression.
//   - tally/copy-after-user-without-chown checks missing --chown after USER.
//     Could both fire on the same stage, but the conditions are different
//     (named-in-passwdless vs missing-chown-after-user). Complementary.
//   - tally/user-created-but-never-used checks created-but-unswitched users.
//     Only fires when final stage stays root; this rule fires on named USER
//     usage in passwd-less stages. Complementary.
type NamedIdentityInPasswdlessStageRule struct{}

// NewNamedIdentityInPasswdlessStageRule creates a new rule instance.
func NewNamedIdentityInPasswdlessStageRule() *NamedIdentityInPasswdlessStageRule {
	return &NamedIdentityInPasswdlessStageRule{}
}

// Metadata returns the rule metadata.
func (r *NamedIdentityInPasswdlessStageRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NamedIdentityInPasswdlessStageRuleCode,
		Name:            "Named Identity in Passwd-less Stage",
		Description:     "Named user/group in USER or --chown requires /etc/passwd which passwd-less stages lack",
		DocURL:          rules.TallyDocURL(NamedIdentityInPasswdlessStageRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
	}
}

// namedIdentityCtx carries shared state for checking a single stage.
type namedIdentityCtx struct {
	file      string
	sm        *sourcemap.SourceMap
	meta      rules.RuleMetadata
	stageIdx  int
	hasPasswd bool
	hasGroup  bool
}

// Check runs the named-identity-in-passwdless-stage rule.
func (r *NamedIdentityInPasswdlessStageRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sem := input.Semantic
	fileFacts := input.Facts
	sm := input.SourceMap()

	var violations []rules.Violation

	for stageIdx := range sem.StageCount() {
		info := sem.StageInfo(stageIdx)
		if info == nil || info.IsWindows() {
			continue
		}

		if !isPasswdlessStage(sem, fileFacts, stageIdx) {
			continue
		}

		sf := fileFacts.Stage(stageIdx)
		hasPasswd, hasGroup := stageHasIdentityDBs(sf, info.Stage)

		// If both databases exist, no named identity issue.
		if hasPasswd && hasGroup {
			continue
		}

		ctx := namedIdentityCtx{
			file:      input.File,
			sm:        sm,
			meta:      meta,
			stageIdx:  stageIdx,
			hasPasswd: hasPasswd,
			hasGroup:  hasGroup,
		}

		// Track whether a SHELL instruction has been seen. After SHELL,
		// the user may have bootstrapped tools that handle identity resolution.
		shellSeen := false

		for _, cmd := range info.Stage.Commands {
			if _, ok := cmd.(*instructions.ShellCommand); ok {
				shellSeen = true
				continue
			}

			if shellSeen {
				continue
			}

			switch c := cmd.(type) {
			case *instructions.UserCommand:
				if v := r.checkUserCmd(c, &ctx); v != nil {
					violations = append(violations, *v)
				}

			case *instructions.CopyCommand:
				if v := r.checkChown(c.Chown, cmd, command.Copy, &ctx); v != nil {
					violations = append(violations, *v)
				}

			case *instructions.AddCommand:
				if v := r.checkChown(c.Chown, cmd, command.Add, &ctx); v != nil {
					violations = append(violations, *v)
				}
			}
		}
	}

	return violations
}

// checkUserCmd checks a USER instruction for named identities in a passwd-less stage.
func (r *NamedIdentityInPasswdlessStageRule) checkUserCmd(
	userCmd *instructions.UserCommand,
	ctx *namedIdentityCtx,
) *rules.Violation {
	user, group := splitUserGroup(userCmd.User)

	namedUser := !ctx.hasPasswd && isNamedIdentity(user)
	namedGroup := !ctx.hasGroup && isNamedIdentity(group)

	if !namedUser && !namedGroup {
		return nil
	}

	loc := rules.NewLocationFromRanges(ctx.file, userCmd.Location())
	msg := namedIdentityMessage(strings.ToUpper(command.User), user, group, namedUser, namedGroup)

	v := rules.NewViolation(loc, ctx.meta.Code, msg, ctx.meta.DefaultSeverity).
		WithDocURL(ctx.meta.DocURL).
		WithDetail(
			"Named identities require /etc/passwd and /etc/group for resolution. " +
				"In scratch or passwd-less stages, use numeric UIDs/GIDs instead (e.g., USER 65532:65532), " +
				"or COPY the passwd/group files from a builder stage.",
		)
	v.StageIndex = ctx.stageIdx

	if fix := buildUserFix(userCmd, ctx, user, group, namedUser, namedGroup); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

// checkChown checks a COPY/ADD --chown flag for named identities.
func (r *NamedIdentityInPasswdlessStageRule) checkChown(
	chown string,
	cmd instructions.Command,
	instruction string,
	ctx *namedIdentityCtx,
) *rules.Violation {
	if chown == "" {
		return nil
	}

	user, group := splitUserGroup(chown)

	namedUser := !ctx.hasPasswd && isNamedIdentity(user)
	namedGroup := !ctx.hasGroup && isNamedIdentity(group)

	if !namedUser && !namedGroup {
		return nil
	}

	loc := rules.NewLocationFromRanges(ctx.file, cmd.Location())
	msg := namedIdentityMessage(strings.ToUpper(instruction)+" --chown", user, group, namedUser, namedGroup)

	v := rules.NewViolation(loc, ctx.meta.Code, msg, ctx.meta.DefaultSeverity).
		WithDocURL(ctx.meta.DocURL).
		WithDetail(
			"Named identities in --chown require /etc/passwd and /etc/group for resolution. " +
				"In scratch or passwd-less stages, use numeric UIDs/GIDs instead (e.g., --chown=65532:65532), " +
				"or COPY the passwd/group files from a builder stage.",
		)
	v.StageIndex = ctx.stageIdx

	if fix := buildChownFix(cmd, chown, ctx, user, group, namedUser, namedGroup); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

// buildUserFix builds a suggested fix that replaces named identities with numeric ones.
func buildUserFix(
	userCmd *instructions.UserCommand,
	ctx *namedIdentityCtx,
	user, group string,
	namedUser, namedGroup bool,
) *rules.SuggestedFix {
	locs := userCmd.Location()
	if len(locs) == 0 {
		return nil
	}

	replacement := numericReplacement(user, group, namedUser, namedGroup)
	newInstruction := strings.ToUpper(command.User) + " " + replacement + "\n"

	line := locs[0].Start.Line
	endLine := ctx.sm.ResolveEndLine(locs[0].End.Line)

	return &rules.SuggestedFix{
		Description: "Replace with numeric identity: USER " + replacement,
		Safety:      rules.FixSuggestion,
		Priority:    ctx.meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(ctx.file, line, 0, endLine+1, 0),
			NewText:  newInstruction,
		}},
	}
}

// buildChownFix builds a suggested fix for --chown with named identities.
func buildChownFix(
	cmd instructions.Command,
	chown string,
	ctx *namedIdentityCtx,
	user, group string,
	namedUser, namedGroup bool,
) *rules.SuggestedFix {
	locs := cmd.Location()
	if len(locs) == 0 {
		return nil
	}

	// Find the --chown flag in the source text to do a targeted replacement.
	line := locs[0].Start.Line
	srcLine := ctx.sm.Line(line - 1) // SourceMap is 0-based, parser locations are 1-based

	oldChown := "--chown=" + chown
	idx := strings.Index(srcLine, oldChown)
	if idx < 0 {
		return nil
	}

	replacement := numericReplacement(user, group, namedUser, namedGroup)
	newChown := "--chown=" + replacement

	return &rules.SuggestedFix{
		Description: "Replace with numeric identity: --chown=" + replacement,
		Safety:      rules.FixSuggestion,
		Priority:    ctx.meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(ctx.file, line, idx, line, idx+len(oldChown)),
			NewText:  newChown,
		}},
	}
}

// numericReplacement computes the numeric UID:GID replacement string.
func numericReplacement(user, group string, namedUser, namedGroup bool) string {
	const defaultNumericID = "65532"

	newUser := user
	if namedUser {
		newUser = defaultNumericID
	}

	newGroup := group
	if namedGroup {
		newGroup = defaultNumericID
	}

	if newGroup != "" {
		return newUser + ":" + newGroup
	}
	return newUser
}

// namedIdentityMessage formats a violation message describing the named identity issue.
func namedIdentityMessage(context, user, group string, namedUser, namedGroup bool) string {
	switch {
	case namedUser && namedGroup:
		return fmt.Sprintf(
			"%s uses named user %q and group %q but this stage has no /etc/passwd or /etc/group",
			context, user, group,
		)
	case namedUser:
		return fmt.Sprintf(
			"%s uses named user %q but this stage has no /etc/passwd",
			context, user,
		)
	default:
		return fmt.Sprintf(
			"%s uses named group %q but this stage has no /etc/group",
			context, group,
		)
	}
}

// isNamedIdentity returns true if the value is a non-empty, non-numeric,
// non-root identity that would require /etc/passwd or /etc/group lookup.
// "root" (UID 0) is built into the kernel and works without passwd files.
func isNamedIdentity(value string) bool {
	if value == "" || isNumericUser(value) {
		return false
	}
	// "root" is universally available without /etc/passwd.
	lower := strings.ToLower(value)
	return lower != "root"
}

// splitUserGroup splits a "user:group" string into its components.
// Returns (user, "") if no colon is present.
func splitUserGroup(value string) (string, string) {
	value = strings.TrimSpace(value)
	user, group, _ := strings.Cut(value, ":")
	return strings.TrimSpace(user), strings.TrimSpace(group)
}

// stageHasIdentityDBs checks whether a stage has /etc/passwd and /etc/group,
// both through the facts layer's observable files and by scanning COPY/ADD
// destinations directly (which catches cross-stage copies from external images
// that the facts layer cannot observe into).
func stageHasIdentityDBs(sf *facts.StageFacts, stage *instructions.Stage) (hasPasswd, hasGroup bool) {
	// Check observable files first (works for heredoc writes, context copies, etc.).
	if sf != nil {
		hasPasswd = sf.HasObservablePathSuffix("/etc/passwd")
		hasGroup = sf.HasObservablePathSuffix("/etc/group")
	}
	if hasPasswd && hasGroup {
		return hasPasswd, hasGroup
	}

	// Scan COPY/ADD destinations for /etc/passwd and /etc/group.
	// This catches COPY --from=builder /etc/passwd /etc/passwd where the
	// builder is an external image (facts cannot observe external image files).
	if stage != nil {
		for _, cmd := range stage.Commands {
			var destPath string
			switch c := cmd.(type) {
			case *instructions.CopyCommand:
				destPath = c.DestPath
			case *instructions.AddCommand:
				destPath = c.DestPath
			default:
				continue
			}
			if !hasPasswd && looksLikePasswdDest(destPath, "/etc/passwd") {
				hasPasswd = true
			}
			if !hasGroup && looksLikePasswdDest(destPath, "/etc/group") {
				hasGroup = true
			}
			if hasPasswd && hasGroup {
				return hasPasswd, hasGroup
			}
		}
	}

	return hasPasswd, hasGroup
}

// looksLikePasswdDest checks if a COPY/ADD destination path produces the
// given target file. Handles both exact paths and directory destinations.
func looksLikePasswdDest(dest, target string) bool {
	if dest == "" || target == "" {
		return false
	}
	// Exact match.
	if dest == target || dest == target+"/" {
		return true
	}
	// Directory destination: /etc/ with source /etc/passwd → /etc/passwd.
	if strings.HasSuffix(dest, "/") && strings.HasPrefix(target, dest) {
		return true
	}
	return false
}

// isPasswdlessStage determines whether a stage lacks /etc/passwd and /etc/group
// databases. This is true for:
//   - FROM scratch (always passwd-less)
//   - Stages inheriting from a scratch ancestry chain without passwd files
//
// It is NOT true for external images (debian, alpine, distroless, etc.) which
// ship their own /etc/passwd.
func isPasswdlessStage(sem *semantic.Model, fileFacts *facts.FileFacts, stageIdx int) bool {
	visited := make(map[int]bool)

	for idx := stageIdx; !visited[idx]; {
		visited[idx] = true

		info := sem.StageInfo(idx)
		if info == nil {
			return false
		}

		// scratch is always passwd-less.
		if info.IsScratch() {
			return true
		}

		// External images (alpine, debian, distroless, etc.) ship /etc/passwd.
		if info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
			return false
		}

		// Local stage ref: check if parent stage produced passwd files
		// via observable files or direct COPY/ADD destination inspection.
		parentIdx := info.BaseImage.StageIndex
		if fileFacts != nil {
			parentFacts := fileFacts.Stage(parentIdx)
			if parentFacts != nil && parentFacts.HasObservablePathSuffix("/etc/passwd") {
				return false
			}
		}
		// Also check the parent's COPY/ADD destinations directly, because
		// COPY --from=<external> may not produce observable files.
		if parentIdx >= 0 && parentIdx < sem.StageCount() {
			parentInfo := sem.StageInfo(parentIdx)
			if parentInfo != nil && parentInfo.Stage != nil {
				hasPasswd, _ := stageHasIdentityDBs(fileFacts.Stage(parentIdx), parentInfo.Stage)
				if hasPasswd {
					return false
				}
			}
		}

		// Walk up the chain.
		idx = parentIdx
	}

	return false
}

func init() {
	rules.Register(NewNamedIdentityInPasswdlessStageRule())
}
