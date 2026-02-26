package shellcheck

import (
	"strings"

	dfparser "github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/sourcemap"
)

type scriptMapping struct {
	// Script is the shell script passed to ShellCheck (without the injected prelude).
	Script string

	// OriginStartLine is the 1-based Dockerfile line that corresponds to Script line 1.
	OriginStartLine int

	// FallbackLine is the 1-based Dockerfile line to use when mapping fails.
	FallbackLine int
}

type blankLeadingFlagsFunc func(lines []string, escapeToken rune) []string

func extractRunScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	return extractRunLikeScript(sm, node, escapeToken, blankRunLeadingFlags)
}

func extractOnbuildRunScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	return extractRunLikeScript(sm, node, escapeToken, blankOnbuildRunLeadingFlags)
}

func extractRunLikeScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
	blankLeadingFlags blankLeadingFlagsFunc,
) (scriptMapping, bool) {
	if sm == nil || node == nil || node.StartLine <= 0 {
		return scriptMapping{}, false
	}

	start := node.StartLine
	end := resolveEndLine(sm, node.EndLine, escapeToken)
	end = max(end, start)

	// Heredoc: lint heredoc body only (exclude the instruction line and terminator).
	if len(node.Heredocs) > 0 {
		bodyStart := start + 1
		bodyEnd := end - 1
		if bodyEnd < bodyStart {
			return scriptMapping{}, false
		}
		lines := linesForSpan(sm, bodyStart, bodyEnd)
		return scriptMapping{
			Script:          strings.Join(lines, "\n"),
			OriginStartLine: bodyStart,
			FallbackLine:    start,
		}, true
	}

	lines := linesForSpan(sm, start, end)
	lines = blankLeadingFlags(lines, escapeToken)
	lines = normalizeContinuationToken(lines, escapeToken)

	return scriptMapping{
		Script:          strings.Join(lines, "\n"),
		OriginStartLine: start,
		FallbackLine:    start,
	}, true
}

func extractShellFormScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
	keyword string,
) (scriptMapping, bool) {
	if sm == nil || node == nil || node.StartLine <= 0 {
		return scriptMapping{}, false
	}

	start := node.StartLine
	end := resolveEndLine(sm, node.EndLine, escapeToken)
	end = max(end, start)

	lines := linesForSpan(sm, start, end)
	lines = blankLeadingKeywordOnly(lines, keyword)
	lines = normalizeContinuationToken(lines, escapeToken)

	return scriptMapping{
		Script:          strings.Join(lines, "\n"),
		OriginStartLine: start,
		FallbackLine:    start,
	}, true
}

func extractHealthcheckCmdShellScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (scriptMapping, bool) {
	if sm == nil || node == nil || node.StartLine <= 0 {
		return scriptMapping{}, false
	}

	start := node.StartLine
	end := resolveEndLine(sm, node.EndLine, escapeToken)
	end = max(end, start)

	lines := linesForSpan(sm, start, end)

	out, ok := blankHealthcheckCmdShellLeading(lines, escapeToken)
	if !ok {
		return scriptMapping{}, false
	}
	out = normalizeContinuationToken(out, escapeToken)

	return scriptMapping{
		Script:          strings.Join(out, "\n"),
		OriginStartLine: start,
		FallbackLine:    start,
	}, true
}

func linesForSpan(sm *sourcemap.SourceMap, startLine, endLine int) []string {
	if sm == nil || startLine <= 0 || endLine < startLine {
		return nil
	}
	lineCount := sm.LineCount()
	if startLine > lineCount {
		return nil
	}
	endLine = min(endLine, lineCount)
	out := make([]string, 0, endLine-startLine+1)
	for l := startLine; l <= endLine; l++ {
		out = append(out, sm.Line(l-1))
	}
	return out
}

func resolveEndLine(sm *sourcemap.SourceMap, endLine int, escapeToken rune) int {
	if sm == nil {
		return endLine
	}

	endLine = min(endLine, sm.LineCount())
	for l := endLine; l <= sm.LineCount(); l++ {
		line := sm.Line(l - 1) // l is 1-based, Line is 0-based
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" || !strings.HasSuffix(trimmed, string(escapeToken)) {
			return l
		}
		endLine = min(l+1, sm.LineCount())
	}
	return endLine
}

// normalizeContinuationToken rewrites Dockerfile line continuations that use a
// non-shell escape token (e.g. backtick) into a POSIX shell line continuation
// (backslash) while preserving columns.
func normalizeContinuationToken(lines []string, escapeToken rune) []string {
	if escapeToken == '\\' || len(lines) == 0 {
		return lines
	}

	out := slicesClone(lines)
	for i, line := range out {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			continue
		}
		lastIdx := len(trimmed) - 1
		if rune(trimmed[lastIdx]) != escapeToken {
			continue
		}
		b := []byte(line)
		b[lastIdx] = '\\'
		out[i] = string(b)
	}
	return out
}

func slicesClone(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
