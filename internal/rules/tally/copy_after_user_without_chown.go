package tally

import (
	"fmt"
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// CopyAfterUserWithoutChownRuleCode is the full rule code.
const CopyAfterUserWithoutChownRuleCode = rules.TallyRulePrefix + "copy-after-user-without-chown"

// CopyAfterUserWithoutChownRule detects COPY/ADD instructions that follow a
// non-root USER without --chown. Docker's COPY and ADD always create files as
// 0:0 (root:root) regardless of the active USER instruction. Authors commonly
// assume USER carries ownership to subsequent COPY/ADD, creating silent
// ownership bugs.
//
// Two fix alternatives are offered per violation:
//
//  1. Add --chown=<user> to the COPY/ADD instruction (preferred).
//  2. Move the USER instruction down past the COPY/ADD to just before the
//     first RUN/WORKDIR (a semantic no-op that clarifies intent).
//
// The rule is suppressed when a subsequent RUN chown already manages
// ownership of the COPY/ADD destination path.
//
// Cross-rule interaction:
//
//   - tally/prefer-copy-chmod (priority 99) also inserts flags on COPY. Both
//     rules use zero-width insertions at the same column; these compose cleanly
//     via the adjacency rule (aEnd <= bStart).
//   - tally/user-created-but-never-used inserts a USER instruction. Our rule
//     fires on COPY/ADD that already follow an existing non-root USER, so
//     the two rules operate on different edit locations. Complementary.
type CopyAfterUserWithoutChownRule struct{}

// NewCopyAfterUserWithoutChownRule creates a new rule instance.
func NewCopyAfterUserWithoutChownRule() *CopyAfterUserWithoutChownRule {
	return &CopyAfterUserWithoutChownRule{}
}

// Metadata returns the rule metadata.
func (r *CopyAfterUserWithoutChownRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            CopyAfterUserWithoutChownRuleCode,
		Name:            "COPY/ADD after non-root USER without --chown",
		Description:     "COPY/ADD without --chown after USER creates root-owned files",
		DocURL:          rules.TallyDocURL(CopyAfterUserWithoutChownRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		FixPriority:     99, // Match prefer-copy-chmod for COPY flag insertion
	}
}

// Check runs the rule across all stages.
func (r *CopyAfterUserWithoutChownRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	fileFacts := input.Facts
	sem := input.Semantic

	violations := make([]rules.Violation, 0, len(input.Stages))

	for stageIdx, stage := range input.Stages {
		// Skip Windows stages: --chown is silently ignored on Windows containers,
		// so suggesting it would conflict with tally/windows/no-chown-flag.
		if info := sem.StageInfo(stageIdx); info != nil && info.IsWindows() {
			continue
		}
		violations = append(violations,
			r.checkStage(stageIdx, stage, fileFacts, sem, input.File, sm, meta)...)
	}

	return violations
}

// userState tracks the effective USER at each point during stage command iteration.
type userState struct {
	user               string
	cmd                *instructions.UserCommand // nil when inherited
	cmdIdx             int
	hasRunOrWorkdirGap bool // true if RUN/WORKDIR between USER and current cmd
}

// copyChownCtx holds shared context passed through the check pipeline.
type copyChownCtx struct {
	stageIdx int
	stage    instructions.Stage
	file     string
	sm       *sourcemap.SourceMap
	meta     rules.RuleMetadata
	variant  shell.Variant
	workdir  string
}

// checkStage walks a single stage tracking the effective user per instruction.
func (r *CopyAfterUserWithoutChownRule) checkStage(
	stageIdx int,
	stage instructions.Stage,
	fileFacts *facts.FileFacts,
	sem *semantic.Model,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) []rules.Violation {
	ctx := copyChownCtx{
		stageIdx: stageIdx,
		stage:    stage,
		file:     file,
		sm:       sm,
		meta:     meta,
		variant:  stageShellVariantForCopyChown(sem, stageIdx),
		workdir:  inheritedWorkdirForCopyChown(fileFacts, sem, stageIdx),
	}

	us := userState{
		user:   inheritedUserForCopyChown(sem, fileFacts, stageIdx),
		cmdIdx: -1,
	}

	var violations []rules.Violation

	for cmdIdx, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.UserCommand:
			us.user = c.User
			us.cmd = c
			us.cmdIdx = cmdIdx
			us.hasRunOrWorkdirGap = false

		case *instructions.WorkdirCommand:
			ctx.workdir = facts.ResolveWorkdir(ctx.workdir, c.Path)
			us.hasRunOrWorkdirGap = true

		case *instructions.RunCommand:
			us.hasRunOrWorkdirGap = true

		case *instructions.CopyCommand:
			if v := r.checkCopyOrAdd(
				cmdIdx, c.Chown, c.DestPath, c.Location(), command.Copy, us, &ctx,
			); v != nil {
				violations = append(violations, *v)
			}

		case *instructions.AddCommand:
			if v := r.checkCopyOrAdd(
				cmdIdx, c.Chown, c.DestPath, c.Location(), command.Add, us, &ctx,
			); v != nil {
				violations = append(violations, *v)
			}
		}
	}

	return violations
}

// checkCopyOrAdd evaluates a single COPY or ADD instruction.
func (r *CopyAfterUserWithoutChownRule) checkCopyOrAdd(
	cmdIdx int,
	chown, destPath string,
	loc []parser.Range,
	keyword string,
	us userState,
	ctx *copyChownCtx,
) *rules.Violation {
	if chown != "" || us.user == "" || facts.IsRootUser(us.user) {
		return nil
	}

	// Suppress when a following RUN chown covers this destination.
	dest := resolveDestForChownCheck(destPath, ctx.workdir)
	if dest != "" && hasFollowingRunTargetingPath(ctx.stage, cmdIdx, "chown", dest, ctx.variant) {
		return nil
	}

	instrLoc := rules.NewLocationFromRanges(ctx.file, loc)
	upperKeyword := strings.ToUpper(keyword)

	v := rules.NewViolation(instrLoc, ctx.meta.Code,
		fmt.Sprintf(
			"%s without --chown creates root-owned files despite USER %s",
			upperKeyword, us.user,
		),
		ctx.meta.DefaultSeverity,
	).WithDocURL(ctx.meta.DocURL).WithDetail(fmt.Sprintf(
		"Docker's %s always creates files as root:root regardless of the active USER. "+
			"Add --chown=%s to match the intended ownership, "+
			"or move USER after the %s to clarify that USER does not affect %s ownership.",
		upperKeyword, us.user, upperKeyword, upperKeyword,
	))
	v.StageIndex = ctx.stageIdx

	fixes := r.buildFixes(loc, keyword, us, cmdIdx, ctx)
	v = v.WithSuggestedFixes(fixes)

	return &v
}

// buildFixes returns the fix alternatives for a violation.
func (r *CopyAfterUserWithoutChownRule) buildFixes(
	instrLoc []parser.Range,
	keyword string,
	us userState,
	cmdIdx int,
	ctx *copyChownCtx,
) []*rules.SuggestedFix {
	var fixes []*rules.SuggestedFix

	// Alt 1 (preferred): add --chown=<user>.
	if fix := r.buildChownFix(instrLoc, keyword, us.user, ctx); fix != nil {
		fixes = append(fixes, fix)
	}

	// Alt 2: move USER down past COPY/ADD to the first RUN/WORKDIR.
	// Only offered when the USER instruction is explicit in this stage,
	// no RUN/WORKDIR exists between USER and the COPY/ADD, and a
	// RUN/WORKDIR exists after the COPY/ADD to serve as the target.
	if us.cmd != nil && !us.hasRunOrWorkdirGap {
		if fix := r.buildMoveUserFix(us, cmdIdx, ctx); fix != nil {
			fixes = append(fixes, fix)
		}
	}

	return fixes
}

// buildChownFix creates the preferred fix: insert --chown=<user> flag.
func (r *CopyAfterUserWithoutChownRule) buildChownFix(
	instrLoc []parser.Range,
	keyword, user string,
	ctx *copyChownCtx,
) *rules.SuggestedFix {
	if len(instrLoc) == 0 {
		return nil
	}

	line := instrLoc[0].Start.Line
	insertCol := findInstructionFlagInsertCol(ctx.sm, line, keyword)
	chownFlag := "--chown=" + user + " "

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Add --chown=%s to %s", user, strings.ToUpper(keyword)),
		Safety:      rules.FixSafe,
		IsPreferred: true,
		Priority:    ctx.meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(ctx.file, line, insertCol, line, insertCol),
			NewText:  chownFlag,
		}},
	}
}

// buildMoveUserFix creates the alternative fix: move the USER instruction
// down to just before the first RUN or WORKDIR that follows the COPY/ADD.
func (r *CopyAfterUserWithoutChownRule) buildMoveUserFix(
	us userState,
	cmdIdx int,
	ctx *copyChownCtx,
) *rules.SuggestedFix {
	target := findNextRunOrWorkdir(ctx.stage, cmdIdx)
	if len(target) == 0 {
		return nil
	}

	targetLine := target[0].Start.Line

	userLoc := us.cmd.Location()
	if len(userLoc) == 0 {
		return nil
	}

	userLine := userLoc[0].Start.Line
	userEndLine := userLoc[0].End.Line

	userLineLen := 0
	totalLines := 0
	if ctx.sm != nil {
		userEndLine = ctx.sm.ResolveEndLine(userEndLine)
		userLineLen = len(ctx.sm.Line(userEndLine - 1))
		totalLines = ctx.sm.LineCount()
	}

	deleteEdit := rules.TextEdit{
		Location: rules.DeleteLineLocation(ctx.file, userLine, userLineLen, totalLines),
		NewText:  "",
	}

	insertEdit := rules.TextEdit{
		Location: rules.NewRangeLocation(ctx.file, targetLine, 0, targetLine, 0),
		NewText:  fmt.Sprintf("USER %s\n", us.user),
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf(
			"Move USER %s after COPY/ADD (USER does not affect COPY/ADD ownership)",
			us.user,
		),
		Safety:   rules.FixSafe,
		Priority: ctx.meta.FixPriority,
		Edits:    []rules.TextEdit{deleteEdit, insertEdit},
	}
}

// findNextRunOrWorkdir finds the first RUN or WORKDIR instruction after
// cmdIdx in the stage, returning its location ranges.
func findNextRunOrWorkdir(stage instructions.Stage, afterCmdIdx int) []parser.Range {
	for i := afterCmdIdx + 1; i < len(stage.Commands); i++ {
		switch c := stage.Commands[i].(type) {
		case *instructions.RunCommand:
			return c.Location()
		case *instructions.WorkdirCommand:
			return c.Location()
		}
	}
	return nil
}

// findInstructionFlagInsertCol finds the column position right after the
// instruction keyword and its trailing space (e.g., after "COPY " or "ADD ").
func findInstructionFlagInsertCol(sm *sourcemap.SourceMap, line int, keyword string) int {
	keywordLen := len(keyword) + 1 // keyword + trailing space
	if sm == nil {
		return keywordLen
	}

	lineText := sm.Line(line - 1) // 0-based
	upper := strings.ToUpper(lineText)
	target := strings.ToUpper(keyword) + " "
	if idx := strings.Index(upper, target); idx >= 0 {
		return idx + keywordLen
	}
	if idx := strings.Index(upper, strings.ToUpper(keyword)); idx >= 0 {
		return idx + keywordLen
	}

	return keywordLen
}

// resolveDestForChownCheck resolves a COPY/ADD destination path against
// the workdir for use in chown suppression matching.
func resolveDestForChownCheck(destPath, workdir string) string {
	if destPath == "" {
		return ""
	}

	dest := destPath
	if !path.IsAbs(dest) {
		dest = path.Join(workdir, dest)
	}

	return path.Clean(dest)
}

// inheritedUserForCopyChown returns the effective user inherited from a
// parent stage via FROM. Returns empty string (root) for external base images.
func inheritedUserForCopyChown(sem *semantic.Model, fileFacts *facts.FileFacts, stageIdx int) string {
	return firstParentStageRefValue(sem, stageIdx, func(parentIdx int) (string, bool) {
		parentFacts := fileFacts.Stage(parentIdx)
		if parentFacts == nil || parentFacts.EffectiveUser == "" {
			return "", false
		}
		return parentFacts.EffectiveUser, true
	})
}

// inheritedWorkdirForCopyChown returns the effective workdir inherited from a
// parent stage via FROM. Unlike inheritedUserForCopyChown, only the immediate
// parent is checked because FinalWorkdir already accounts for the full chain.
func inheritedWorkdirForCopyChown(fileFacts *facts.FileFacts, sem *semantic.Model, stageIdx int) string {
	info := sem.StageInfo(stageIdx)
	if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
		return "/"
	}

	if parentFacts := fileFacts.Stage(info.BaseImage.StageIndex); parentFacts != nil {
		if parentFacts.FinalWorkdir != "" {
			return parentFacts.FinalWorkdir
		}
	}

	return "/"
}

// stageShellVariantForCopyChown returns the shell variant for a stage.
func stageShellVariantForCopyChown(sem *semantic.Model, stageIdx int) shell.Variant {
	if info := sem.StageInfo(stageIdx); info != nil {
		return info.ShellSetting.Variant
	}
	return shell.VariantBash
}

func init() {
	rules.Register(NewCopyAfterUserWithoutChownRule())
}
