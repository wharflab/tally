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
	m.Script = extract.NormalizeContinuation(m.Script, escapeToken, '`')
	return scriptMapping{
		Script:            m.Script,
		OriginStartLine:   m.OriginStartLine,
		FallbackLine:      m.FallbackLine,
		ShellNameOverride: m.ShellNameOverride,
	}, true
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
	if exe != "powershell" && exe != "pwsh" {
		return explicitPowerShellInvocation{}, false
	}

	firstTokenAfterExe := true
	for {
		tokenStart := shellutil.SkipShellTokenSpaces(script, next)
		token, end := shellutil.NextShellToken(script, next)
		if token == "" {
			return explicitPowerShellInvocation{}, false
		}

		tokenNorm := strings.ToLower(shellutil.DropQuotes(token))
		if tokenNorm == "-command" || tokenNorm == "-c" {
			return invocationFromRemainder(script, end)
		}

		if firstTokenAfterExe && !strings.HasPrefix(tokenNorm, "-") {
			return invocationFromRemainder(script, tokenStart)
		}

		next = end
		firstTokenAfterExe = false
	}
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
