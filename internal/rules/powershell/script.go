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
		if isPowerShellTerminalSwitch(tokenNorm) {
			return explicitPowerShellInvocation{}, false
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
			if isPowerShellTerminalSwitch(tokenNorm) {
				return explicitPowerShellInvocation{}, false
			}
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
	return powerShellSwitchMatchesAny(token, powerShellSwitchSpec{match: "command", smallest: "c"})
}

func isPowerShellCommandWithArgsSwitch(token string) bool {
	return powerShellSwitchMatchesAny(token,
		powerShellSwitchSpec{match: "commandwithargs", smallest: "commandwithargs"},
		powerShellSwitchSpec{match: "cwa", smallest: "cwa"},
	)
}

func isPowerShellContinuationToken(token string) bool {
	return token == `\` || token == "`"
}

func isPowerShellFileModeSwitch(token string) bool {
	return powerShellSwitchMatchesAny(token,
		powerShellSwitchSpec{match: "file", smallest: "f"},
		powerShellSwitchSpec{match: "encodedcommand", smallest: "e"},
		powerShellSwitchSpec{match: "ec", smallest: "e"},
	)
}

func isPowerShellTerminalSwitch(token string) bool {
	return powerShellSwitchMatchesAny(token,
		powerShellSwitchSpec{match: "help", smallest: "h"},
		powerShellSwitchSpec{match: "?", smallest: "?"},
		powerShellSwitchSpec{match: "version", smallest: "v"},
	)
}

func powerShellOptionConsumesNextToken(token string) bool {
	if powerShellOptionHasInlineValue(token) {
		return false
	}
	return powerShellSwitchMatchesAny(token,
		powerShellSwitchSpec{match: "configurationname", smallest: "config"},
		powerShellSwitchSpec{match: "configurationfile", smallest: "configurationfile"},
		powerShellSwitchSpec{match: "custompipename", smallest: "cus"},
		powerShellSwitchSpec{match: "encodedarguments", smallest: "encodeda"},
		powerShellSwitchSpec{match: "ea", smallest: "ea"},
		powerShellSwitchSpec{match: "executionpolicy", smallest: "ex"},
		powerShellSwitchSpec{match: "ep", smallest: "ep"},
		powerShellSwitchSpec{match: "inputformat", smallest: "inp"},
		powerShellSwitchSpec{match: "if", smallest: "if"},
		powerShellSwitchSpec{match: "outputformat", smallest: "o"},
		powerShellSwitchSpec{match: "of", smallest: "o"},
		powerShellSwitchSpec{match: "psconsolefile", smallest: "psconsolefile"},
		powerShellSwitchSpec{match: "settingsfile", smallest: "settings"},
		powerShellSwitchSpec{match: "windowstyle", smallest: "w"},
		powerShellSwitchSpec{match: "workingdirectory", smallest: "wo"},
		powerShellSwitchSpec{match: "wd", smallest: "wd"},
	)
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

type powerShellSwitchSpec struct {
	match    string
	smallest string
}

func powerShellSwitchMatchesAny(token string, specs ...powerShellSwitchSpec) bool {
	key, ok := powerShellSwitchKey(token)
	if !ok {
		return false
	}
	for _, spec := range specs {
		if powerShellSwitchMatches(key, spec.match, spec.smallest) {
			return true
		}
	}
	return false
}

func powerShellSwitchKey(token string) (string, bool) {
	name := powerShellOptionName(token)
	if !strings.HasPrefix(name, "-") {
		return "", false
	}
	key := strings.TrimLeft(name, "-")
	return key, key != ""
}

func powerShellSwitchMatches(key, match, smallest string) bool {
	return len(key) >= len(smallest) && strings.HasPrefix(match, key)
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
