package buildkit

import (
	"path"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

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
				// Relative WORKDIR without prior absolute WORKDIR
				loc := rules.NewLocationFromRanges(input.File, workdir.Location())
				detail := "Set an absolute WORKDIR before using relative paths, " +
					"e.g., 'WORKDIR /app' before 'WORKDIR " + workdir.Path + "'"
				violations = append(violations, rules.NewViolation(
					loc,
					r.Metadata().Code,
					"Relative workdir "+workdir.Path+
						" can have unexpected results if the base image has a WORKDIR set",
					r.Metadata().DefaultSeverity,
				).WithDocURL(r.Metadata().DocURL).WithDetail(detail))
			}
			// If workdirSet is already true, relative paths are fine
			// (they're relative to the known absolute path)
		}
	}

	return violations
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
