package shell

import (
	"regexp"
	"strings"
)

var cmdExitPattern = regexp.MustCompile(`(?i)(^|[&()\r\n \t])exit(?:[ \t\r\n]|$)`)

func countPowerShellStatements(script string) int {
	analysis := analyzePowerShellScript(script)
	if analysis == nil {
		return 0
	}
	if analysis.HasComplex {
		if len(analysis.Statements) > 0 {
			return 1
		}
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
	return cmdExitPattern.MatchString(script)
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

	if strings.Contains(script, "||") {
		return nil, false
	}

	parts := splitCmdAndCommands(script)
	if len(parts) == 0 {
		return nil, false
	}

	if len(parts) != len(analysis.Commands) {
		return nil, false
	}
	return parts, true
}

func splitCmdAndCommands(script string) []string {
	var (
		parts   []string
		current strings.Builder
		quote   byte
	)

	for i := 0; i < len(script); i++ {
		ch := script[i]

		if quote != 0 {
			current.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}

		switch ch {
		case '"', '\'':
			quote = ch
			current.WriteByte(ch)
		case '&':
			if i+1 < len(script) && script[i+1] == '&' {
				part := strings.TrimSpace(current.String())
				if part != "" {
					parts = append(parts, part)
				}
				current.Reset()
				i++
				continue
			}
			current.WriteByte(ch)
		default:
			current.WriteByte(ch)
		}
	}

	part := strings.TrimSpace(current.String())
	if part != "" {
		parts = append(parts, part)
	}

	return parts
}
