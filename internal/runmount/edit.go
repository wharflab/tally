package runmount

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/sourcemap"
)

// RunKeywordEndColumn returns the zero-based column immediately after the RUN
// keyword and following whitespace. This is the stable insertion point for
// Dockerfile-level RUN flags such as --mount.
func RunKeywordEndColumn(runLoc []parser.Range, sm *sourcemap.SourceMap) int {
	if len(runLoc) == 0 {
		return 4 //nolint:mnd // len("RUN ")
	}

	if sm != nil && runLoc[0].Start.Line > 0 {
		line := sm.Line(runLoc[0].Start.Line - 1)
		if col, ok := runKeywordEndColumn(line); ok {
			return col
		}
		return len(leadingWhitespace(line)) + 4 //nolint:mnd // len("RUN ")
	}

	return runLoc[0].Start.Character + 4 //nolint:mnd // len("RUN ")
}

func runKeywordEndColumn(line string) (int, bool) {
	start := len(leadingWhitespace(line))
	if col, ok := keywordEndAfterWhitespace(line, start, command.Run); ok {
		return col, true
	}

	onbuildEnd, ok := keywordEnd(line, start, command.Onbuild)
	if !ok {
		return 0, false
	}
	runStart := skipHorizontalWhitespace(line, onbuildEnd)
	return keywordEndAfterWhitespace(line, runStart, command.Run)
}

func keywordEnd(line string, start int, keyword string) (int, bool) {
	end := start + len(keyword)
	if start < 0 || end > len(line) || !strings.EqualFold(line[start:end], keyword) {
		return 0, false
	}
	return end, true
}

func keywordEndAfterWhitespace(line string, start int, keyword string) (int, bool) {
	end, ok := keywordEnd(line, start, keyword)
	if !ok || end >= len(line) || !isHorizontalWhitespace(line[end]) {
		return 0, false
	}
	return end + 1, true
}

func skipHorizontalWhitespace(line string, start int) int {
	for start < len(line) && isHorizontalWhitespace(line[start]) {
		start++
	}
	return start
}

func isHorizontalWhitespace(b byte) bool {
	return b == ' ' || b == '\t'
}

func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
