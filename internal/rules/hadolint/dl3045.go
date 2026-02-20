package hadolint

import (
	"strings"
	"unicode"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
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
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3045",
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practice",
		IsExperimental:  false,
	}
}

// Check runs the DL3045 rule.
// It warns when a COPY instruction uses a relative destination path without
// a WORKDIR having been set in the current stage (or inherited from a parent stage).
func (r *DL3045Rule) Check(input rules.LintInput) []rules.Violation {
	var violations []rules.Violation
	meta := r.Metadata()

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}

	// Track whether WORKDIR has been set per stage key.
	workdirSet := make(map[string]bool)

	for stageIdx, stage := range input.Stages {
		stageKey := stageKeyFromStage(&stage)
		workdirSet[stageKey] = inheritWorkdirStatus(sem, stageIdx, workdirSet)

		for _, cmd := range stage.Commands {
			if _, ok := cmd.(*instructions.WorkdirCommand); ok {
				workdirSet[stageKey] = true
			}

			if v := checkCopyDest(cmd, workdirSet[stageKey], input.File, meta); v != nil {
				violations = append(violations, *v)
			}
		}

		violations = append(violations, checkOnbuildCopies(sem, stageIdx, workdirSet[stageKey], input.File, meta)...)
	}

	return violations
}

// stageKeyFromStage returns a lowercase key for the stage (alias if set, otherwise base name).
func stageKeyFromStage(stage *instructions.Stage) string {
	if stage.Name != "" {
		return strings.ToLower(stage.Name)
	}
	return stage.BaseName
}

// inheritWorkdirStatus checks if the stage inherits WORKDIR from a parent stage.
func inheritWorkdirStatus(sem *semantic.Model, stageIdx int, workdirSet map[string]bool) bool {
	if sem == nil {
		return false
	}
	info := sem.StageInfo(stageIdx)
	if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef {
		return false
	}
	parentStage := sem.Stage(info.BaseImage.StageIndex)
	if parentStage == nil {
		return false
	}
	return workdirSet[stageKeyFromStage(parentStage)]
}

// checkCopyDest checks a single command; if it is a COPY with a relative
// destination and WORKDIR is not set, it returns a violation.
func checkCopyDest(cmd instructions.Command, hasWorkdir bool, file string, meta rules.RuleMetadata) *rules.Violation {
	copyCmd, ok := cmd.(*instructions.CopyCommand)
	if !ok {
		return nil
	}
	if hasWorkdir {
		return nil
	}
	if isAbsoluteOrVariableDest(copyCmd.DestPath) {
		return nil
	}
	v := newDL3045Violation(file, copyCmd.Location(), meta)
	return &v
}

// checkOnbuildCopies checks ONBUILD COPY instructions in a stage.
func checkOnbuildCopies(sem *semantic.Model, stageIdx int, hasWorkdir bool, file string, meta rules.RuleMetadata) []rules.Violation {
	if sem == nil {
		return nil
	}

	var violations []rules.Violation
	onbuildHasWorkdir := hasWorkdir

	for _, onbuild := range sem.OnbuildInstructions(stageIdx) {
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
		violations = append(violations, newDL3045Violation(file, copyCmd.Location(), meta))
	}

	return violations
}

func newDL3045Violation(file string, loc []parser.Range, meta rules.RuleMetadata) rules.Violation {
	return rules.NewViolation(
		rules.NewLocationFromRanges(file, loc),
		meta.Code,
		"`COPY` to a relative destination without `WORKDIR` set",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithDetail(
		"When no WORKDIR is set, the working directory defaults to `/` but this is implicit " +
			"and fragile. Either set WORKDIR before COPY or use an absolute destination path.",
	)
}

// isAbsoluteOrVariableDest checks if a COPY destination is absolute, a Windows
// absolute path, or an environment variable reference. Surrounding quotes are
// stripped before checking.
func isAbsoluteOrVariableDest(dest string) bool {
	dest = shell.DropQuotes(dest)

	// Absolute Unix path.
	if strings.HasPrefix(dest, "/") {
		return true
	}

	// Environment variable reference.
	if strings.HasPrefix(dest, "$") {
		return true
	}

	// Windows absolute path (e.g., "c:\..." or "C:/...").
	return isWindowsAbsolute(dest)
}

// isWindowsAbsolute checks if a path is a Windows absolute path (e.g., "c:\..." or "D:/...").
func isWindowsAbsolute(path string) bool {
	if len(path) < 2 {
		return false
	}
	return unicode.IsLetter(rune(path[0])) && path[1] == ':'
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3045Rule())
}
