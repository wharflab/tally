package directive

import "github.com/wharflab/tally/internal/sourcemap"

// nextInstructionLineRange returns the line range of the next Dockerfile instruction.
//
// If spanIndex is nil, it falls back to nextNonCommentLineRange (legacy behavior).
//
// Line numbers are 0-based.
func nextInstructionLineRange(line int, sm *sourcemap.SourceMap, spanIndex *InstructionSpanIndex) LineRange {
	if sm == nil {
		return LineRange{Start: -1, End: -1}
	}
	if spanIndex == nil {
		return nextNonCommentLineRange(line, sm)
	}

	if span, ok := spanIndex.nextInstructionSpan(line); ok {
		return LineRange{Start: span.StartLine, End: span.EndLine}
	}
	return LineRange{Start: -1, End: -1}
}
