package buildkit

import (
	"fmt"
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// WorkdirRelativePathRule implements the WorkdirRelativePath linting rule.
// It detects relative WORKDIR instructions that appear before any absolute
// WORKDIR has been set in the stage.
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
// It tracks whether an absolute WORKDIR has been set for each stage and
// warns if a relative WORKDIR is used before any absolute path is set.
func (r *WorkdirRelativePathRule) Check(input rules.LintInput) []rules.Violation {
	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}
	meta := r.Metadata()
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		// Determine OS for path checking using the semantic model.
		isWindows := false
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				isWindows = info.IsWindows()
			}
		}

		// Track if an absolute WORKDIR has been set in this stage.
		// A stage inherits the WORKDIR from its base image, but we can't
		// know that value statically, so we only track within the stage.
		workdirSet := false

		for _, cmd := range stage.Commands {
			workdir, ok := cmd.(*instructions.WorkdirCommand)
			if !ok {
				continue
			}

			if isAbsPath(workdir.Path, isWindows) {
				workdirSet = true
			} else if !workdirSet {
				// Relative WORKDIR without prior absolute WORKDIR.
				loc := rules.NewLocationFromRanges(input.File, workdir.Location())
				v := rules.NewViolation(
					loc,
					meta.Code,
					"Relative workdir "+workdir.Path+
						" can have unexpected results if the base image has a WORKDIR set",
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(
					"Set an absolute WORKDIR before using relative paths, " +
						"e.g., 'WORKDIR /app' before 'WORKDIR " + workdir.Path + "'",
				).WithSuggestedFix(workdirRelativePathFix(input.File, workdir))
				v.StageIndex = stageIdx
				violations = append(violations, v)
			}
			// If workdirSet is already true, relative paths are fine
			// (they're relative to the known absolute path)
		}
	}

	return violations
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

	// Only plan async checks for stages that have violations.
	stagesWithViolations := make(map[int]bool)
	for stageIdx, stage := range input.Stages {
		isWindows := false
		if info := sem.StageInfo(stageIdx); info != nil {
			isWindows = info.IsWindows()
		}
		workdirSet := false
		for _, cmd := range stage.Commands {
			workdir, ok := cmd.(*instructions.WorkdirCommand)
			if !ok {
				continue
			}
			if isAbsPath(workdir.Path, isWindows) {
				workdirSet = true
			} else if !workdirSet {
				stagesWithViolations[stageIdx] = true
			}
		}
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

	out := make([]any, 0)

	allStages := []int{h.stageIdx}
	allStages = append(allStages, findWorkdirDescendants(h.semantic, h.stageIdx)...)

	for _, idx := range allStages {
		if !h.stagesWithViolations[idx] {
			continue
		}

		// Suppress fast-path violations.
		out = append(out, async.CompletedCheck{
			RuleCode:   h.meta.Code,
			File:       h.file,
			StageIndex: idx,
		})

		// Re-emit with resolved absolute paths.
		out = append(out, h.refineStageViolations(idx, baseDir)...)
	}

	return out
}

// refineStageViolations re-derives violations for a stage and attaches
// fixes that resolve relative WORKDIRs to absolute paths.
func (h *workdirRelPathHandler) refineStageViolations(stageIdx int, baseDir string) []any {
	if stageIdx >= len(h.stages) {
		return nil
	}

	isWindows := false
	if h.semantic != nil {
		if info := h.semantic.StageInfo(stageIdx); info != nil {
			isWindows = info.IsWindows()
		}
	}

	out := make([]any, 0)
	workdirSet := false
	// Track cumulative path to resolve chained relative WORKDIRs.
	// e.g. WORKDIR foo / WORKDIR bar → /base/foo, /base/foo/bar
	currentDir := baseDir

	for _, cmd := range h.stages[stageIdx].Commands {
		workdir, ok := cmd.(*instructions.WorkdirCommand)
		if !ok {
			continue
		}

		if isAbsPath(workdir.Path, isWindows) {
			workdirSet = true
			currentDir = workdir.Path
			continue
		}

		if workdirSet {
			// Relative after an explicit absolute — Docker resolves it; no violation.
			currentDir = path.Join(currentDir, workdir.Path)
			continue
		}

		// Violation: relative WORKDIR without prior absolute.
		resolvedPath := path.Clean(path.Join(currentDir, workdir.Path))
		currentDir = resolvedPath

		loc := rules.NewLocationFromRanges(h.file, workdir.Location())

		var detail string
		if cfg := baseDir; cfg != "/" {
			detail = fmt.Sprintf(
				"Base image sets WORKDIR to %q. Resolving relative path %q gives %q.",
				baseDir, workdir.Path, resolvedPath,
			)
		} else {
			detail = fmt.Sprintf(
				"Base image has no WORKDIR (defaults to /). Resolving relative path %q gives %q.",
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
			WithSuggestedFix(workdirRelativePathFixResolved(h.file, workdir, resolvedPath))
		v.StageIndex = stageIdx
		out = append(out, v)
	}

	return out
}

// findWorkdirDescendants returns descendant stage indices that transitively
// inherit from the given stage without setting their own absolute WORKDIR.
func findWorkdirDescendants(sem *semantic.Model, stageIdx int) []int {
	result := make([]int, 0)
	for i := range sem.StageCount() {
		info := sem.StageInfo(i)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef {
			continue
		}
		if info.BaseImage.StageIndex != stageIdx {
			continue
		}
		result = append(result, i)
		result = append(result, findWorkdirDescendants(sem, i)...)
	}
	return result
}

// workdirRelativePathFix creates a fast-path fix that replaces the relative
// WORKDIR with an absolute guess. Since we don't know the base image's WORKDIR,
// we resolve against "/" as a reasonable default.
func workdirRelativePathFix(file string, workdir *instructions.WorkdirCommand) *rules.SuggestedFix {
	loc := workdir.Location()
	if len(loc) == 0 {
		return nil
	}
	resolvedPath := path.Clean("/" + workdir.Path)
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
func workdirRelativePathFixResolved(file string, workdir *instructions.WorkdirCommand, resolvedPath string) *rules.SuggestedFix {
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

// isAbsPath checks if a path is absolute, accounting for Windows drive-letter paths
// when isWindows is true. This matches BuildKit's system.IsAbs logic.
func isAbsPath(p string, isWindows bool) bool {
	if isWindows {
		// Windows paths: C:\, C:/, \\server\share, or / (forward slash is valid on Windows too)
		if len(p) >= 1 && (p[0] == '/' || p[0] == '\\') {
			return true
		}
		// Drive letter: C:\ or C:/
		if len(p) >= 3 && p[1] == ':' && (p[2] == '/' || p[2] == '\\') {
			return true
		}
		return false
	}
	// Unix/Linux: starts with /
	return path.IsAbs(p)
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewWorkdirRelativePathRule())
}
