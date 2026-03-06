package extract

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	dfparser "github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/sourcemap"
)

type Mapping struct {
	// Script is the extracted script body with Dockerfile syntax blanked out so
	// mvdan positions line up with the original Dockerfile columns.
	Script string

	// OriginStartLine is the 1-based Dockerfile line corresponding to script line 1.
	OriginStartLine int

	// FallbackLine is the 1-based Dockerfile line to use if precise mapping fails.
	FallbackLine int

	// IsHeredoc reports whether the script came from a heredoc body.
	IsHeredoc bool
}

func ExtractRunScript(sm *sourcemap.SourceMap, node *dfparser.Node, escapeToken rune) (Mapping, bool) {
	return extractRunLikeScript(sm, node, escapeToken, blankRunLeadingFlags)
}

func ExtractOnbuildRunScript(sm *sourcemap.SourceMap, node *dfparser.Node, escapeToken rune) (Mapping, bool) {
	return extractRunLikeScript(sm, node, escapeToken, blankOnbuildRunLeadingFlags)
}

func ExtractShellFormScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
	keyword string,
) (Mapping, bool) {
	if sm == nil || node == nil || node.StartLine <= 0 {
		return Mapping{}, false
	}

	start := node.StartLine
	end := sm.ResolveEndLineWithEscape(node.EndLine, escapeToken)
	end = max(end, start)

	lines := linesForSpan(sm, start, end)
	lines = blankLeadingKeywordOnly(lines, keyword, escapeToken)
	lines = normalizeContinuationToken(lines, escapeToken)

	return Mapping{
		Script:          strings.Join(lines, "\n"),
		OriginStartLine: start,
		FallbackLine:    start,
	}, true
}

func ExtractHealthcheckCmdShellScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
) (Mapping, bool) {
	if sm == nil || node == nil || node.StartLine <= 0 {
		return Mapping{}, false
	}

	start := node.StartLine
	end := sm.ResolveEndLineWithEscape(node.EndLine, escapeToken)
	end = max(end, start)

	lines := linesForSpan(sm, start, end)
	out, ok := blankHealthcheckCmdShellLeading(lines, escapeToken)
	if !ok {
		return Mapping{}, false
	}
	out = normalizeContinuationToken(out, escapeToken)

	return Mapping{
		Script:          strings.Join(out, "\n"),
		OriginStartLine: start,
		FallbackLine:    start,
	}, true
}

func InitialShellNameForStage(
	stage instructions.Stage,
	directives []directive.ShellDirective,
	stageInfo *semantic.StageInfo,
) string {
	shellName := semantic.DefaultShell[0]

	fromLine := -1
	if len(stage.Location) > 0 {
		fromLine = stage.Location[0].Start.Line - 1
	}
	if fromLine >= 0 {
		bestLine := -1
		for i := range directives {
			sd := directives[i]
			if sd.Line < fromLine && sd.Line > bestLine {
				bestLine = sd.Line
				shellName = sd.Shell
			}
		}
	}

	if stageInfo != nil &&
		stageInfo.ShellSetting.Source == semantic.ShellSourceDefault &&
		len(stageInfo.ShellSetting.Shell) > 0 {
		shellName = stageInfo.ShellSetting.Shell[0]
	}
	return shellName
}

func CommandStartLine(location []dfparser.Range) int {
	if len(location) == 0 {
		return 0
	}
	return location[0].Start.Line
}

func extractRunLikeScript(
	sm *sourcemap.SourceMap,
	node *dfparser.Node,
	escapeToken rune,
	blankLeadingFlags func(lines []string, escapeToken rune) []string,
) (Mapping, bool) {
	if sm == nil || node == nil || node.StartLine <= 0 {
		return Mapping{}, false
	}

	start := node.StartLine
	end := sm.ResolveEndLineWithEscape(node.EndLine, escapeToken)
	end = max(end, start)

	if len(node.Heredocs) > 0 {
		bodyStart := start + 1
		bodyEnd := end - 1
		if bodyEnd < bodyStart {
			return Mapping{}, false
		}
		lines := linesForSpan(sm, bodyStart, bodyEnd)
		return Mapping{
			Script:          strings.Join(lines, "\n"),
			OriginStartLine: bodyStart,
			FallbackLine:    start,
			IsHeredoc:       true,
		}, true
	}

	lines := linesForSpan(sm, start, end)
	lines = blankLeadingFlags(lines, escapeToken)
	lines = normalizeContinuationToken(lines, escapeToken)

	return Mapping{
		Script:          strings.Join(lines, "\n"),
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
	for line := startLine; line <= endLine; line++ {
		out = append(out, sm.Line(line-1))
	}
	return out
}

func normalizeContinuationToken(lines []string, escapeToken rune) []string {
	if escapeToken == '\\' || len(lines) == 0 {
		return lines
	}

	out := slices.Clone(lines)
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
