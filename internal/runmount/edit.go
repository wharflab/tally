package runmount

import (
	"strings"

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
		// Search for "RUN " in the line to handle both plain RUN and ONBUILD RUN.
		if idx := strings.Index(strings.ToUpper(line), "RUN "); idx >= 0 {
			return idx + 4 //nolint:mnd // len("RUN ")
		}
		return len(leadingWhitespace(line)) + 4 //nolint:mnd // len("RUN ")
	}

	return runLoc[0].Start.Character + 4 //nolint:mnd // len("RUN ")
}

func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
