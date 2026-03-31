package rules

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/shell"
)

// GitRunContext captures the effective shell context for a RUN instruction.
type GitRunContext struct {
	Script  string
	Workdir string
	Variant shell.Variant
}

// BuildGitRunContexts returns per-RUN git analysis context sourced from facts.
func BuildGitRunContexts(input LintInput) map[*instructions.RunCommand]GitRunContext {
	contexts := make(map[*instructions.RunCommand]GitRunContext)
	if input.Facts == nil {
		return contexts
	}

	for stageIdx := range input.Stages {
		stageFacts := input.Facts.Stage(stageIdx)
		if stageFacts == nil {
			continue
		}
		for _, runFacts := range stageFacts.Runs {
			if runFacts == nil || runFacts.Run == nil {
				continue
			}
			contexts[runFacts.Run] = GitRunContext{
				Script:  runFacts.SourceScript,
				Workdir: runFacts.Workdir,
				Variant: runFacts.Shell.Variant,
			}
		}
	}

	return contexts
}
