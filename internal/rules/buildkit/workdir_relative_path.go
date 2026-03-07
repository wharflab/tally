package buildkit

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/util/system"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/asyncutil"
	"github.com/wharflab/tally/internal/semantic"
)

// WorkdirRelativePathRule implements the WorkdirRelativePath linting rule.
// It detects the first relative WORKDIR instruction in a stage when no
// absolute WORKDIR has been set yet (matching upstream BuildKit semantics).
type WorkdirRelativePathRule struct{}

// NewWorkdirRelativePathRule creates a new WorkdirRelativePath rule instance.
func NewWorkdirRelativePathRule() *WorkdirRelativePathRule {
	return &WorkdirRelativePathRule{}
}

// Metadata returns the rule metadata.
func (r *WorkdirRelativePathRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.BuildKitRulePrefix + "WorkdirRelativePath",
		Name:            "Relative WORKDIR Path",
		Description:     "Relative WORKDIR path used without a base absolute path",
		DocURL:          rules.BuildKitDocURL("WorkdirRelativePath"),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Check runs the WorkdirRelativePath rule.
func (r *WorkdirRelativePathRule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}
	return findRelativeWorkdirViolations(sem, input.Stages, input.File, r.Metadata())
}

// findRelativeWorkdirViolations iterates all stages and returns a violation
// for the first relative WORKDIR in each stage that lacks a prior absolute
// WORKDIR. Matches upstream BuildKit semantics: workdirSet is flipped to true
// on any WORKDIR (absolute or relative), so only the first relative triggers.
func findRelativeWorkdirViolations(
	sem *semantic.Model,
	stages []instructions.Stage,
	file string,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation
	// Track per-stage workdirSet for inheritance.
	hasWorkdir := make([]bool, len(stages))

	for stageIdx, stage := range stages {
		platformOS := stageOS(sem, stageIdx)
		hasWorkdir[stageIdx] = inheritedWorkdirSet(sem, stageIdx, hasWorkdir)

		for _, cmd := range stage.Commands {
			workdir, ok := cmd.(*instructions.WorkdirCommand)
			if !ok {
				continue
			}

			if !hasWorkdir[stageIdx] && !system.IsAbs(workdir.Path, platformOS) {
				loc := rules.NewLocationFromRanges(file, workdir.Location())
				v := rules.NewViolation(
					loc,
					meta.Code,
					"Relative workdir "+workdir.Path+
						" can have unexpected results if the base image has a WORKDIR set",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"Set an absolute WORKDIR before using relative paths, " +
						"e.g., 'WORKDIR /app' before 'WORKDIR " + workdir.Path + "'",
				).WithSuggestedFix(workdirRelativePathFix(file, workdir, platformOS))
				v.StageIndex = stageIdx
				violations = append(violations, v)
			}
			// Upstream BuildKit sets workdirSet = true for ANY WORKDIR
			// (absolute or relative), so only the first relative triggers.
			hasWorkdir[stageIdx] = true
		}
	}

	return violations
}

// inheritedWorkdirSet returns true if the stage inherits workdirSet from a
// parent stage referenced via FROM <stage>. Matches upstream BuildKit's
// ds.workdirSet = ds.base.workdirSet inheritance.
func inheritedWorkdirSet(sem *semantic.Model, stageIdx int, hasWorkdir []bool) bool {
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

// PlanAsync creates check requests to resolve base image config for external
// images. When resolved, the handler replaces fast-path fix suggestions with
// precise ones that resolve the relative WORKDIR against the actual base path.
func (r *WorkdirRelativePathRule) PlanAsync(input rules.LintInput) []async.CheckRequest {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok || sem == nil {
		return nil
	}

	meta := r.Metadata()

	violations := findRelativeWorkdirViolations(sem, input.Stages, input.File, meta)
	stagesWithViolations := make(map[int]bool, len(violations))
	for _, v := range violations {
		stagesWithViolations[v.StageIndex] = true
	}

	if len(stagesWithViolations) == 0 {
		return nil
	}

	return planExternalImageChecks(input, meta, func(
		m rules.RuleMetadata,
		info *semantic.StageInfo,
		file, _ string,
	) async.ResultHandler {
		return &workdirRelPathHandler{
			meta:                 m,
			file:                 file,
			stageIdx:             info.Index,
			semantic:             sem,
			stages:               input.Stages,
			stagesWithViolations: stagesWithViolations,
		}
	})
}

// workdirRelPathHandler replaces fast-path violations with refined fix
// suggestions that resolve relative WORKDIRs against the actual base image path.
type workdirRelPathHandler struct {
	meta                 rules.RuleMetadata
	file                 string
	stageIdx             int
	semantic             *semantic.Model
	stages               []instructions.Stage
	stagesWithViolations map[int]bool
}

func (h *workdirRelPathHandler) OnSuccess(resolved any) []any {
	cfg, ok := resolved.(*registry.ImageConfig)
	if !ok || cfg == nil {
		return nil
	}

	// Determine the effective base dir: resolved WorkingDir or "/" (Docker default).
	baseDir := cfg.WorkingDir
	if baseDir == "" {
		baseDir = "/"
	}

	// Reject paths with characters that could inject Dockerfile instructions.
	if strings.ContainsAny(baseDir, "\n\r\x00") {
		return nil
	}

	descendants := h.semantic.FromDescendants(h.stageIdx, nil)
	allStages := make([]int, 0, 1+len(descendants))
	allStages = append(allStages, h.stageIdx)
	allStages = append(allStages, descendants...)

	return asyncutil.RefinedViolations(
		h.meta.Code, h.file, allStages,
		func(idx int) bool { return h.stagesWithViolations[idx] },
		func(idx int) []any { return h.refineStageViolations(idx, baseDir) },
	)
}

// refineStageViolations re-derives the violation for the first relative
// WORKDIR in the stage and attaches a fix using the resolved base path.
func (h *workdirRelPathHandler) refineStageViolations(stageIdx int, baseDir string) []any {
	if stageIdx >= len(h.stages) {
		return nil
	}

	platformOS := stageOS(h.semantic, stageIdx)

	out := make([]any, 0)
	// refineStageViolations is only called for stages that had violations,
	// meaning workdirSet was false at the point of the first relative WORKDIR.
	// We don't need inheritance here — it's already accounted for by the
	// caller (stagesWithViolations was derived from findRelativeWorkdirViolations).
	workdirSet := false

	for _, cmd := range h.stages[stageIdx].Commands {
		workdir, ok := cmd.(*instructions.WorkdirCommand)
		if !ok {
			continue
		}

		if !workdirSet && !system.IsAbs(workdir.Path, platformOS) {
			resolvedPath, err := system.NormalizeWorkdir(baseDir, workdir.Path, platformOS)
			if err != nil {
				break // can't resolve — skip the fix
			}

			loc := rules.NewLocationFromRanges(h.file, workdir.Location())

			var detail string
			if baseDir != "/" {
				detail = fmt.Sprintf(
					"Base image sets WORKDIR to %q. Resolving relative path %q gives %q.",
					baseDir, workdir.Path, resolvedPath,
				)
			} else {
				detail = fmt.Sprintf(
					"Base image has no WORKDIR (defaults to /). "+
						"Resolving relative path %q gives %q.",
					workdir.Path, resolvedPath,
				)
			}

			v := rules.NewViolation(
				loc,
				h.meta.Code,
				"Relative workdir "+workdir.Path+
					" can have unexpected results if the base image has a WORKDIR set",
				h.meta.DefaultSeverity,
			).WithDocURL(h.meta.DocURL).WithDetail(detail).
				WithSuggestedFix(
					workdirRelativePathFixResolved(h.file, workdir, resolvedPath),
				)
			v.StageIndex = stageIdx
			out = append(out, v)
		}
		// Match upstream: workdirSet = true for any WORKDIR.
		workdirSet = true
	}

	return out
}

// stageOS returns the platform OS string for a stage ("windows" or "linux").
// Defaults to empty string (interpreted as Linux by system.NormalizeWorkdir).
func stageOS(sem *semantic.Model, stageIdx int) string {
	if sem == nil {
		return ""
	}
	info := sem.StageInfo(stageIdx)
	if info == nil {
		return ""
	}
	if info.IsWindows() {
		return "windows"
	}
	return ""
}

// workdirRelativePathFix creates a fast-path fix that replaces the relative
// WORKDIR with an absolute path resolved against "/" (the default when no
// base image WORKDIR is known).
func workdirRelativePathFix(
	file string,
	workdir *instructions.WorkdirCommand,
	platformOS string,
) *rules.SuggestedFix {
	loc := workdir.Location()
	if len(loc) == 0 {
		return nil
	}
	resolvedPath, err := system.NormalizeWorkdir("/", workdir.Path, platformOS)
	if err != nil {
		return nil
	}
	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Replace with absolute path `WORKDIR %s`", resolvedPath),
		Safety:      rules.FixSuggestion,
		Edits: []rules.TextEdit{{
			Location: rules.NewLocationFromRanges(file, loc),
			NewText:  "WORKDIR " + resolvedPath,
		}},
	}
}

// workdirRelativePathFixResolved creates a registry-backed fix that replaces
// the relative WORKDIR with the resolved absolute path. Since the path is
// computed from the actual base image WORKDIR, the fix is safe to apply.
func workdirRelativePathFixResolved(
	file string,
	workdir *instructions.WorkdirCommand,
	resolvedPath string,
) *rules.SuggestedFix {
	loc := workdir.Location()
	if len(loc) == 0 {
		return nil
	}
	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Replace with absolute path `WORKDIR %s`", resolvedPath),
		Safety:      rules.FixSafe,
		Edits: []rules.TextEdit{{
			Location: rules.NewLocationFromRanges(file, loc),
			NewText:  "WORKDIR " + resolvedPath,
		}},
	}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewWorkdirRelativePathRule())
}
