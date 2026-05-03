package powershell

import (
	"strings"

	dfparser "github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/highlight/extract"
	shellutil "github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

type scriptMapping struct {
	Script            string
	OriginStartLine   int
	OriginStartColumn int
	FallbackLine      int
	ShellNameOverride string
	CanFix            bool
}

type explicitPowerShellInvocation struct {
	script      string
	startLine   int
	startColumn int
}

func extractRunScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	m, ok := extract.ExtractRunScript(sm, node, escapeToken)
	if !ok {
		return scriptMapping{}, false
	}
	return scriptMappingFromExtract(m, escapeToken), true
}

func extractOnbuildRunScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	m, ok := extract.ExtractOnbuildRunScript(sm, node, escapeToken)
	if !ok {
		return scriptMapping{}, false
	}
	return scriptMappingFromExtract(m, escapeToken), true
}

func scriptMappingFromExtract(m extract.Mapping, escapeToken rune) scriptMapping {
	if !m.IsHeredoc {
		m.Script = extract.NormalizeContinuation(m.Script, escapeToken, '`')
	}
	return scriptMapping{
		Script:            m.Script,
		OriginStartLine:   m.OriginStartLine,
		OriginStartColumn: m.OriginStartColumn,
		FallbackLine:      m.FallbackLine,
		ShellNameOverride: m.ShellNameOverride,
		CanFix:            true,
	}
}

func parseExplicitPowerShellInvocation(script string) (explicitPowerShellInvocation, bool) {
	i := shellutil.SkipShellTokenSpaces(script, 0)
	if i >= len(script) {
		return explicitPowerShellInvocation{}, false
	}
	if script[i] == '@' {
		i++
		i = shellutil.SkipShellTokenSpaces(script, i)
	}

	exeToken, next := shellutil.NextShellToken(script, i)
	if exeToken == "" {
		return explicitPowerShellInvocation{}, false
	}
	if !isPowerShellExecutable(shellutil.DropQuotes(exeToken)) {
		return explicitPowerShellInvocation{}, false
	}

	for {
		token, end := shellutil.NextShellToken(script, next)
		if token == "" {
			return explicitPowerShellInvocation{}, false
		}

		tokenNorm := strings.ToLower(shellutil.DropQuotes(token))
		if isPowerShellContinuationToken(tokenNorm) {
			next = end
			continue
		}
		if isPowerShellCommandSwitch(tokenNorm) {
			return invocationFromRemainder(script, end)
		}
		if isPowerShellCommandWithArgsSwitch(tokenNorm) {
			return invocationFromNextToken(script, end)
		}
		if isPowerShellFileModeSwitch(tokenNorm) {
			return explicitPowerShellInvocation{}, false
		}
		if powerShellOptionConsumesNextToken(tokenNorm) {
			valueToken, valueEnd := shellutil.NextShellToken(script, end)
			if valueToken == "" {
				return explicitPowerShellInvocation{}, false
			}
			next = valueEnd
			continue
		}
		if !strings.HasPrefix(tokenNorm, "-") {
			return explicitPowerShellInvocation{}, false
		}

		next = end
	}
}

func parseExecFormPowerShellInvocation(args []string) (explicitPowerShellInvocation, bool) {
	if len(args) == 0 {
		return explicitPowerShellInvocation{}, false
	}
	if !isPowerShellExecutable(args[0]) {
		return explicitPowerShellInvocation{}, false
	}

	for i := 1; i < len(args); i++ {
		tokenNorm := strings.ToLower(args[i])
		if !isPowerShellCommandSwitch(tokenNorm) && !isPowerShellCommandWithArgsSwitch(tokenNorm) {
			if isPowerShellFileModeSwitch(tokenNorm) {
				return explicitPowerShellInvocation{}, false
			}
			if powerShellOptionConsumesNextToken(tokenNorm) {
				i++
				if i >= len(args) {
					return explicitPowerShellInvocation{}, false
				}
				continue
			}
			if !strings.HasPrefix(tokenNorm, "-") {
				return explicitPowerShellInvocation{}, false
			}
			continue
		}
		if i+1 >= len(args) {
			return explicitPowerShellInvocation{}, false
		}
		script := args[i+1]
		if isPowerShellCommandSwitch(tokenNorm) {
			script = strings.Join(args[i+1:], " ")
		}
		script = strings.TrimSpace(script)
		if script == "" {
			return explicitPowerShellInvocation{}, false
		}
		return explicitPowerShellInvocation{script: script}, true
	}

	return explicitPowerShellInvocation{}, false
}

func isPowerShellCommandSwitch(token string) bool {
	return token == "-command" || token == "-c"
}

func isPowerShellCommandWithArgsSwitch(token string) bool {
	return token == "-commandwithargs" || token == "-cwa"
}

func isPowerShellContinuationToken(token string) bool {
	return token == `\` || token == "`"
}

func isPowerShellFileModeSwitch(token string) bool {
	switch powerShellOptionName(token) {
	case "-file", "-f", "-encodedcommand", "-e", "-ec":
		return true
	default:
		return false
	}
}

func powerShellOptionConsumesNextToken(token string) bool {
	if powerShellOptionHasInlineValue(token) {
		return false
	}
	switch powerShellOptionName(token) {
	case "-configurationname",
		"-configurationfile",
		"-custompipename",
		"-encodedarguments",
		"-executionpolicy",
		"-inputformat",
		"-outputformat",
		"-psconsolefile",
		"-settingsfile",
		"-version",
		"-windowstyle",
		"-workingdirectory":
		return true
	default:
		return false
	}
}

func powerShellOptionName(token string) string {
	name := token
	if idx := strings.IndexAny(name, ":="); idx >= 0 {
		name = name[:idx]
	}
	return name
}

func powerShellOptionHasInlineValue(token string) bool {
	return strings.ContainsAny(token, ":=")
}

func isPowerShellExecutable(exe string) bool {
	switch shellutil.NormalizeShellExecutableName(exe) {
	case "powershell", "pwsh":
		return true
	default:
		return false
	}
}

func invocationFromNextToken(script string, start int) (explicitPowerShellInvocation, bool) {
	start = shellutil.SkipShellTokenSpaces(script, start)
	if start >= len(script) {
		return explicitPowerShellInvocation{}, false
	}

	token, _ := shellutil.NextShellToken(script, start)
	if token == "" {
		return explicitPowerShellInvocation{}, false
	}
	if !isQuotedShellToken(token) && isShellControlToken(token) {
		return explicitPowerShellInvocation{}, false
	}

	unquoted := shellutil.DropQuotes(token)
	if strings.TrimSpace(unquoted) == "" {
		return explicitPowerShellInvocation{}, false
	}
	if unquoted != token {
		start++
	}
	line, col := sourcemap.ByteToLineCol(script, start)
	return explicitPowerShellInvocation{
		script:      unquoted,
		startLine:   line,
		startColumn: col,
	}, true
}

func invocationFromRemainder(script string, start int) (explicitPowerShellInvocation, bool) {
	start = shellutil.SkipShellTokenSpaces(script, start)
	if start >= len(script) {
		return explicitPowerShellInvocation{}, false
	}

	end := len(script)
	next := start
	for {
		tokenStart := shellutil.SkipShellTokenSpaces(script, next)
		if tokenStart >= len(script) {
			break
		}

		token, tokenEnd := shellutil.NextShellToken(script, tokenStart)
		if token == "" {
			break
		}
		if !isQuotedShellToken(token) && isShellControlToken(token) {
			end = tokenStart
			break
		}
		next = tokenEnd
	}

	rest := script[start:end]
	trimmed := strings.TrimSpace(rest)
	if trimmed == "" {
		return explicitPowerShellInvocation{}, false
	}
	trimStart := strings.Index(rest, trimmed)
	if trimStart > 0 {
		start += trimStart
	}

	token, end := shellutil.NextShellToken(trimmed, 0)
	if token != "" && end == len(trimmed) {
		unquoted := shellutil.DropQuotes(token)
		if unquoted != token {
			start++
		}
		line, col := sourcemap.ByteToLineCol(script, start)
		return explicitPowerShellInvocation{
			script:      unquoted,
			startLine:   line,
			startColumn: col,
		}, true
	}

	line, col := sourcemap.ByteToLineCol(script, start)
	return explicitPowerShellInvocation{
		script:      trimmed,
		startLine:   line,
		startColumn: col,
	}, true
}

func isQuotedShellToken(token string) bool {
	return strings.HasPrefix(token, `"`) || strings.HasPrefix(token, `'`)
}

func isShellControlToken(token string) bool {
	switch token {
	case "&&", "||", "|", "&", ";":
		return true
	default:
		return false
	}
}
