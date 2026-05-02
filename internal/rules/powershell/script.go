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
	exe := shellutil.NormalizeShellExecutableName(shellutil.DropQuotes(exeToken))
	if exe != "pwsh" {
		return explicitPowerShellInvocation{}, false
	}

	for {
		token, end := shellutil.NextShellToken(script, next)
		if token == "" {
			return explicitPowerShellInvocation{}, false
		}

		tokenNorm := strings.ToLower(shellutil.DropQuotes(token))
		if tokenNorm == "-command" || tokenNorm == "-c" {
			return invocationFromRemainder(script, end)
		}

		next = end
	}
}

func parseExecFormPowerShellInvocation(args []string) (explicitPowerShellInvocation, bool) {
	if len(args) == 0 {
		return explicitPowerShellInvocation{}, false
	}
	exe := shellutil.NormalizeShellExecutableName(args[0])
	if exe != "pwsh" {
		return explicitPowerShellInvocation{}, false
	}

	for i := 1; i < len(args); i++ {
		tokenNorm := strings.ToLower(args[i])
		if tokenNorm != "-command" && tokenNorm != "-c" {
			continue
		}
		if i+1 >= len(args) {
			return explicitPowerShellInvocation{}, false
		}
		script := strings.TrimSpace(strings.Join(args[i+1:], " "))
		if script == "" {
			return explicitPowerShellInvocation{}, false
		}
		return explicitPowerShellInvocation{script: script}, true
	}

	return explicitPowerShellInvocation{}, false
}

func invocationFromRemainder(script string, start int) (explicitPowerShellInvocation, bool) {
	start = shellutil.SkipShellTokenSpaces(script, start)
	if start >= len(script) {
		return explicitPowerShellInvocation{}, false
	}

	rest := script[start:]
	trimmed := strings.TrimSpace(rest)
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
