package powershell

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

// powershellStages returns stages that either run under a PowerShell SHELL
// instruction or explicitly invoke PowerShell from shell-form RUN commands.
func powershellStages(input rules.LintInput) []*semantic.StageInfo {
	sem := input.Semantic

	stages := make([]*semantic.StageInfo, 0, sem.StageCount())
	for i := range sem.StageCount() {
		info := sem.StageInfo(i)
		if info == nil {
			continue
		}
		if info.ShellSetting.Variant.IsPowerShell() {
			stages = append(stages, info)
			continue
		}
		if info.Stage != nil && stageInvokesPowerShell(*info.Stage) {
			stages = append(stages, info)
		}
	}

	return stages
}

func stageInvokesPowerShell(stage instructions.Stage) bool {
	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell || len(run.CmdLine) == 0 {
			continue
		}
		if _, ok := parseExplicitPowerShellInvocation(run.CmdLine[0]); ok {
			return true
		}
	}
	return false
}
