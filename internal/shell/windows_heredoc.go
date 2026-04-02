package shell

import "strings"

func countPowerShellStatements(script string) int {
	analysis := analyzePowerShellScript(script)
	if analysis == nil {
		return 0
	}
	return len(analysis.Statements)
}

func extractPowerShellStatements(script string) []string {
	analysis := analyzePowerShellScript(script)
	if analysis == nil || analysis.HasComplex {
		return nil
	}

	out := make([]string, 0, len(analysis.Statements))
	for _, stmt := range analysis.Statements {
		out = append(out, stmt.Text)
	}
	return out
}

func hasPowerShellPipes(script string) bool {
	analysis := analyzePowerShellScript(script)
	if analysis == nil {
		return false
	}
	for _, stmt := range analysis.Statements {
		if stmt.HasPipe {
			return true
		}
	}
	return false
}

func isSimplePowerShellScript(script string) bool {
	analysis := analyzePowerShellScript(script)
	return analysis != nil && !analysis.HasComplex && len(analysis.Statements) > 0
}

func hasPowerShellExitCommand(script string) bool {
	return hasPowerShellFlowControl(script, cmdExit)
}

func countCmdCommands(script string) int {
	parts, ok := extractCmdStatements(script)
	if ok {
		return len(parts)
	}

	analysis := AnalyzeCmdScript(script)
	if analysis == nil || len(analysis.Commands) == 0 {
		return 0
	}
	return 1
}

func extractCmdCommands(script string) []string {
	parts, ok := extractCmdStatements(script)
	if !ok {
		return nil
	}
	return parts
}

func isSimpleCmdScript(script string) bool {
	_, ok := extractCmdStatements(script)
	return ok
}

func hasCmdExitCommand(script string) bool {
	analysis := AnalyzeCmdScript(script)
	return analysis != nil && analysis.HasExitCommand
}

func hasCmdPipes(script string) bool {
	analysis := AnalyzeCmdScript(script)
	return analysis != nil && analysis.HasPipes
}

func extractCmdStatements(script string) ([]string, bool) {
	analysis := AnalyzeCmdScript(script)
	if analysis == nil || len(analysis.Commands) == 0 {
		return nil, false
	}

	if analysis.HasPipes || analysis.HasRedirections || analysis.HasControlFlow || analysis.HasVariableReferences {
		return nil, false
	}

	return extractCmdStatementsFromAnalysis(script, analysis)
}

func extractCmdStatementsFromAnalysis(script string, analysis *CmdScriptAnalysis) ([]string, bool) {
	if analysis == nil {
		return nil, false
	}
	if len(analysis.commandByteRanges) != len(analysis.Commands) {
		return nil, false
	}
	expectedOps := 0
	if len(analysis.Commands) > 0 {
		expectedOps = len(analysis.Commands) - 1
	}
	if len(analysis.conditionalOps) != expectedOps {
		return nil, false
	}

	parts := make([]string, 0, len(analysis.commandByteRanges))
	var previousEnd uint
	for i, bounds := range analysis.commandByteRanges {
		start, end := bounds[0], bounds[1]
		if start > end || end > uint(len(script)) {
			return nil, false
		}
		if i > 0 && start < previousEnd {
			return nil, false
		}
		if i == 0 {
			if strings.TrimSpace(script[:start]) != "" {
				return nil, false
			}
		} else {
			op := analysis.conditionalOps[i-1]
			if op.Text != "&&" {
				return nil, false
			}
			if op.Start < previousEnd || op.End > start || op.End < op.Start {
				return nil, false
			}
			if strings.TrimSpace(script[previousEnd:op.Start]) != "" {
				return nil, false
			}
			if strings.TrimSpace(script[op.End:start]) != "" {
				return nil, false
			}
		}

		part := strings.TrimSpace(script[start:end])
		if part == "" {
			return nil, false
		}
		parts = append(parts, part)
		previousEnd = end
	}

	if strings.TrimSpace(script[previousEnd:]) != "" {
		return nil, false
	}

	return parts, true
}
