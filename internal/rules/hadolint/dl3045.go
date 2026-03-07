package hadolint

import (
	"fmt"
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/util/system"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/asyncutil"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
)

// DL3045Rule implements the DL3045 linting rule.
type DL3045Rule struct{}

// NewDL3045Rule creates a new DL3045 rule instance.
func NewDL3045Rule() *DL3045Rule {
	return &DL3045Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3045Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3045",
		Name:            "COPY to relative destination without WORKDIR",
		Description:     "`COPY` to a relative destination without `WORKDIR` set",
		DocURL:          rules.HadolintDocURL("DL3045"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// Check runs the DL3045 rule.
// It warns when a COPY instruction uses a relative destination path without
// a WORKDIR having been set in the current stage (or inherited from a parent stage).
func (r *DL3045Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}

	violations, hasWorkdir := findCopyViolations(sem, input.Stages, input.File, meta)

	// Also check ONBUILD COPY instructions.
	for stageIdx := range input.Stages {
		violations = append(violations,
			checkOnbuildCopies(sem, stageIdx, hasWorkdir[stageIdx], input.File, meta)...)
	}

	return violations
}

// PlanAsync creates check requests to resolve base image config for external
// images. When resolved, the handler replaces fast-path fix suggestions with
// precise ones based on the base image's actual WorkingDir.
func (r *DL3045Rule) PlanAsync(input rules.LintInput) []async.CheckRequest {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	meta := r.Metadata()

	violations, _ := findCopyViolations(sem, input.Stages, input.File, meta)
	stagesWithViolations := make(map[int]bool, len(violations))
	for _, v := range violations {
		stagesWithViolations[v.StageIndex] = true
	}

	if len(stagesWithViolations) == 0 {
		return nil
	}

	return asyncutil.PlanExternalImageChecks(input, meta, func(
		m rules.RuleMetadata,
		info *semantic.StageInfo,
		file, _ string,
	) async.ResultHandler {
		return &dl3045Handler{
			meta:                 m,
			file:                 file,
			stageIdx:             info.Index,
			semantic:             sem,
			stages:               input.Stages,
			stagesWithViolations: stagesWithViolations,
		}
	})
}

// dl3045Handler replaces fast-path violations with refined fix suggestions
// based on the resolved base image WorkingDir.
//
// The violation always stands — even when the base image has a WORKDIR, the
// reader still can't see it from the Dockerfile alone (opacity). The registry
// data only improves the fix: instead of guessing "/app", we use the actual
// inherited path, making the fix safe to apply.
type dl3045Handler struct {
	meta                 rules.RuleMetadata
	file                 string
	stageIdx             int
	semantic             *semantic.Model
	stages               []instructions.Stage
	stagesWithViolations map[int]bool
}

func (h *dl3045Handler) OnSuccess(resolved any) []any {
	cfg, ok := resolved.(*registry.ImageConfig)
	if !ok || cfg == nil {
		return nil
	}

	// Determine the effective WorkingDir. Empty means "/" (Docker default).
	baseDir := cfg.WorkingDir
	if baseDir == "" {
		// No WORKDIR in base image — fast-path violations with "/app" guess
		// are the best we can do; nothing to refine.
		return nil
	}

	// Base image has a WORKDIR — replace fast-path violations with refined
	// fixes that use the actual inherited path.
	out := make([]any, 0)

	descendants := h.semantic.FromDescendants(h.stageIdx, func(i int) bool {
		return i < len(h.stages) && stageHasExplicitWorkdir(&h.stages[i])
	})
	allStages := make([]int, 0, 1+len(descendants))
	allStages = append(allStages, h.stageIdx)
	allStages = append(allStages, descendants...)

	for _, idx := range allStages {
		if !h.stagesWithViolations[idx] {
			continue
		}

		// Suppress fast-path violations for this stage.
		out = append(out, async.CompletedCheck{
			RuleCode:   h.meta.Code,
			File:       h.file,
			StageIndex: idx,
		})

		// Re-emit violations with precise fix using resolved WorkingDir.
		out = append(out, h.refineStageViolations(idx, baseDir)...)
	}

	return out
}

// refineStageViolations re-derives violations for a stage and attaches
// fix suggestions that use the resolved baseDir instead of the "/app" guess.
func (h *dl3045Handler) refineStageViolations(stageIdx int, baseDir string) []any {
	if stageIdx >= len(h.stages) {
		return nil
	}

	out := make([]any, 0)
	for _, cmd := range h.stages[stageIdx].Commands {
		copyCmd, ok := cmd.(*instructions.CopyCommand)
		if !ok {
			continue
		}
		if isAbsoluteOrVariableDest(copyCmd.DestPath) {
			continue
		}
		v := rules.NewViolation(
			rules.NewLocationFromRanges(h.file, copyCmd.Location()),
			h.meta.Code,
			"`COPY` to a relative destination without `WORKDIR` set",
			h.meta.DefaultSeverity,
		).WithDocURL(h.meta.DocURL).WithDetail(
			fmt.Sprintf(
				"Base image sets WORKDIR to %q. Making it explicit avoids "+
					"silent breakage if the base image changes its WORKDIR.",
				baseDir,
			),
		).WithSuggestedFix(dl3045FixResolved(h.file, copyCmd.Location(), baseDir))
		v.StageIndex = stageIdx
		out = append(out, v)
	}

	return out
}

// stageHasExplicitWorkdir checks if a stage has an explicit WORKDIR instruction.
func stageHasExplicitWorkdir(stage *instructions.Stage) bool {
	for _, cmd := range stage.Commands {
		if _, ok := cmd.(*instructions.WorkdirCommand); ok {
			return true
		}
	}
	return false
}

// findCopyViolations iterates all stages and returns violations for COPY
// instructions with relative destinations that lack an explicit WORKDIR.
// It also returns the per-stage hasWorkdir state for ONBUILD checking.
func findCopyViolations(
	sem *semantic.Model,
	stages []instructions.Stage,
	file string,
	meta rules.RuleMetadata,
) ([]rules.Violation, []bool) {
	hasWorkdir := make([]bool, len(stages))
	var violations []rules.Violation

	for stageIdx, stage := range stages {
		hasWorkdir[stageIdx] = inheritedWorkdir(sem, stageIdx, hasWorkdir)

		for _, cmd := range stage.Commands {
			if _, ok := cmd.(*instructions.WorkdirCommand); ok {
				hasWorkdir[stageIdx] = true
			}

			if v := checkCopyDest(cmd, hasWorkdir[stageIdx], stageIdx, file, meta); v != nil {
				violations = append(violations, *v)
			}
		}
	}

	return violations, hasWorkdir
}

// inheritedWorkdir returns true if the stage inherits a WORKDIR from a parent
// stage (resolved via the semantic model's BaseImage.StageIndex).
func inheritedWorkdir(sem *semantic.Model, stageIdx int, hasWorkdir []bool) bool {
	if sem == nil {
		return false
	}
	info := sem.StageInfo(stageIdx)
	if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef {
		return false
	}
	parentIdx := info.BaseImage.StageIndex
	if parentIdx < 0 || parentIdx >= len(hasWorkdir) {
		return false
	}
	return hasWorkdir[parentIdx]
}

// checkCopyDest checks a single command; if it is a COPY with a relative
// destination and WORKDIR is not set, it returns a violation with a fix suggestion.
func checkCopyDest(cmd instructions.Command, workdirSet bool, stageIdx int, file string, meta rules.RuleMetadata) *rules.Violation {
	copyCmd, ok := cmd.(*instructions.CopyCommand)
	if !ok {
		return nil
	}
	if workdirSet {
		return nil
	}
	if isAbsoluteOrVariableDest(copyCmd.DestPath) {
		return nil
	}
	v := newDL3045Violation(file, copyCmd.Location(), stageIdx, meta)
	return &v
}

// checkOnbuildCopies checks ONBUILD COPY instructions in a stage.
func checkOnbuildCopies(sem *semantic.Model, stageIdx int, workdirSet bool, file string, meta rules.RuleMetadata) []rules.Violation {
	if sem == nil {
		return nil
	}

	onbuildHasWorkdir := workdirSet
	onbuilds := sem.OnbuildInstructions(stageIdx)
	violations := make([]rules.Violation, 0, len(onbuilds))

	for _, onbuild := range onbuilds {
		if _, ok := onbuild.Command.(*instructions.WorkdirCommand); ok {
			onbuildHasWorkdir = true
		}

		copyCmd, ok := onbuild.Command.(*instructions.CopyCommand)
		if !ok {
			continue
		}
		if onbuildHasWorkdir {
			continue
		}
		if isAbsoluteOrVariableDest(copyCmd.DestPath) {
			continue
		}
		violations = append(violations, newDL3045Violation(file, copyCmd.Location(), stageIdx, meta))
	}

	return violations
}

func newDL3045Violation(file string, loc []parser.Range, stageIdx int, meta rules.RuleMetadata) rules.Violation {
	v := rules.NewViolation(
		rules.NewLocationFromRanges(file, loc),
		meta.Code,
		"`COPY` to a relative destination without `WORKDIR` set",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"When no WORKDIR is set, the working directory defaults to `/` but this is implicit " +
			"and fragile. Either set WORKDIR before COPY or use an absolute destination path.",
	).WithSuggestedFix(dl3045Fix(file, loc))
	v.StageIndex = stageIdx
	return v
}

// dl3045Fix creates a fix suggestion that inserts "WORKDIR /app" before the
// offending COPY instruction. This is the fast-path guess used when no
// registry data is available.
func dl3045Fix(file string, loc []parser.Range) *rules.SuggestedFix {
	if len(loc) == 0 {
		return nil
	}
	insertLine := loc[0].Start.Line
	return &rules.SuggestedFix{
		Description: "Add `WORKDIR /app` before COPY",
		Safety:      rules.FixSuggestion,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, insertLine, 0, insertLine, 0),
			NewText:  "WORKDIR /app\n",
		}},
	}
}

// dl3045FixResolved creates a fix suggestion using the resolved base image
// WorkingDir. Since the path is the actual inherited value, the fix just makes
// the implicit state explicit — it doesn't change behavior, so it's FixSafe.
func dl3045FixResolved(file string, loc []parser.Range, baseDir string) *rules.SuggestedFix {
	if len(loc) == 0 {
		return nil
	}
	dir := path.Clean(baseDir)
	if !isSafePath(dir) {
		return nil // don't generate a fix from untrusted data with control chars
	}
	insertLine := loc[0].Start.Line
	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Add `WORKDIR %s` before COPY (inherited from base image)", dir),
		Safety:      rules.FixSafe,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, insertLine, 0, insertLine, 0),
			NewText:  "WORKDIR " + dir + "\n",
		}},
	}
}

// isSafePath returns false if p contains characters that could inject
// Dockerfile instructions when interpolated into a fix. Dockerfile syntax
// is line-oriented, so the injection vectors are \n, \r, and \0.
func isSafePath(p string) bool {
	return !strings.ContainsAny(p, "\n\r\x00")
}

// isAbsoluteOrVariableDest checks if a COPY destination is absolute (on any
// platform) or an environment variable reference. Surrounding quotes are
// stripped before checking.
//
// Note: system.IsAbs intentionally ignores drive letters (WORKDIR context),
// but Docker COPY does honour them as absolute on Windows containers. We
// check both system.IsAbs and the drive-letter form to avoid false positives.
func isAbsoluteOrVariableDest(dest string) bool {
	dest = shell.DropQuotes(dest)

	// Environment variable reference — not a path, skip.
	if strings.HasPrefix(dest, "$") {
		return true
	}

	// Absolute on Linux or Windows (delegates to BuildKit's system.IsAbs).
	if system.IsAbs(dest, "") || system.IsAbs(dest, "windows") {
		return true
	}

	// Windows drive-letter paths (e.g. "C:\..." or "D:/...") are absolute
	// for COPY destinations even though system.IsAbs strips the drive letter.
	if len(dest) >= 2 && dest[1] == ':' &&
		((dest[0] >= 'A' && dest[0] <= 'Z') || (dest[0] >= 'a' && dest[0] <= 'z')) {
		return true
	}

	return false
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3045Rule())
}
