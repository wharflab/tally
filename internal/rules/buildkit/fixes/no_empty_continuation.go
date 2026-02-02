package fixes

import (
	"bytes"
	"slices"

	"github.com/tinovyatkin/tally/internal/rules"
)

// enrichNoEmptyContinuationFix adds auto-fix for BuildKit's NoEmptyContinuation rule.
// This removes empty lines that appear within multi-line commands (after backslash continuation).
//
// Example:
//
//	RUN apk update && \
//	                       <- empty line to remove
//	    apk add curl
//
// Becomes:
//
//	RUN apk update && \
//	    apk add curl
func enrichNoEmptyContinuationFix(v *rules.Violation, source []byte) {
	// Find all empty continuation lines in the source
	// The warning location points to the last line of the multi-line command
	endLine := v.Location.End.Line
	if endLine <= 0 {
		endLine = v.Location.Start.Line
	}
	if endLine <= 0 {
		return
	}

	// Scan backward to find the start of this multi-line command
	// A line ending with '\' indicates continuation
	lines := splitLines(source)
	if endLine > len(lines) {
		return
	}

	// Find empty lines within multi-line continuations
	emptyLineIndices := findEmptyContinuationLines(lines, endLine-1) // Convert to 0-based
	if len(emptyLineIndices) == 0 {
		return
	}

	// Create edits to remove each empty line
	// To delete a line, we span from start of the empty line to start of the next line.
	// This effectively removes the line including its newline character.
	var edits []rules.TextEdit
	for _, lineIdx := range emptyLineIndices {
		// Line numbers in Location are 1-based
		lineNum := lineIdx + 1
		nextLineNum := lineNum + 1
		// If this is the last line, delete the newline from the previous line
		if lineIdx >= len(lines)-1 {
			if lineIdx == 0 {
				continue
			}
			prevLineLen := len(lines[lineIdx-1])
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(v.Location.File, lineNum-1, prevLineLen, lineNum, 0),
				NewText:  "",
			})
		} else {
			// Span from start of this line to start of next line (removes the line and its newline)
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(v.Location.File, lineNum, 0, nextLineNum, 0),
				NewText:  "",
			})
		}
	}

	v.SuggestedFix = &rules.SuggestedFix{
		Description: "Remove empty continuation line(s)",
		Safety:      rules.FixSafe,
		Edits:       edits,
		IsPreferred: true,
	}
}

// splitLines splits source into lines preserving line content without newlines.
// Handles mixed line endings (LF and CRLF) robustly by splitting after each newline
// and trimming the line ending suffix from each chunk.
func splitLines(source []byte) [][]byte {
	// Split after each newline - this keeps \r\n together for CRLF lines
	chunks := bytes.SplitAfter(source, []byte("\n"))

	lines := make([][]byte, 0, len(chunks))
	for _, chunk := range chunks {
		// Skip empty trailing chunk (SplitAfter creates one if source ends with \n)
		if len(chunk) == 0 {
			continue
		}
		// Trim the line ending suffix (\r\n or \n)
		line := bytes.TrimSuffix(chunk, []byte("\r\n"))
		line = bytes.TrimSuffix(line, []byte("\n"))
		lines = append(lines, line)
	}
	return lines
}

// findEmptyContinuationLines finds empty lines within a multi-line command.
// It scans backward from endLineIdx to find all empty lines that are part of
// the continuation sequence (lines between a line ending with '\' and the next content).
//
// endLineIdx is 0-based line index.
func findEmptyContinuationLines(lines [][]byte, endLineIdx int) []int {
	var emptyIndices []int

	// Check if the end line itself is empty (happens when empty line is at EOF)
	if endLineIdx < len(lines) {
		trimmed := bytes.TrimSpace(lines[endLineIdx])
		if len(trimmed) == 0 && endLineIdx > 0 && hasContinuationBefore(lines, endLineIdx) {
			emptyIndices = append(emptyIndices, endLineIdx)
		}
	}

	// Scan backward from the end line
	// Look for the pattern: content ending with '\', followed by empty line(s)
scanLoop:
	for i := endLineIdx - 1; i >= 0; i-- {
		line := lines[i]
		trimmed := bytes.TrimSpace(line)

		switch {
		case len(trimmed) == 0:
			// This is an empty line - check if it's within a continuation
			// We need to find a preceding line that ends with '\'
			if i > 0 && hasContinuationBefore(lines, i) {
				emptyIndices = append(emptyIndices, i)
			}
		case bytes.HasSuffix(trimmed, []byte("\\")):
			// Found a continuation line - empty lines after this are targets
			// Continue scanning to find more continuations
			continue
		default:
			// Found a non-continuation, non-empty line
			// If it's not part of the same multi-line command, stop
			if !hasContinuationBefore(lines, i) {
				break scanLoop
			}
		}
	}

	// Reverse to get ascending order
	slices.Reverse(emptyIndices)

	return emptyIndices
}

// hasContinuationBefore checks if any preceding non-empty line ends with '\'.
// startIdx is exclusive - scanning starts from startIdx-1.
func hasContinuationBefore(lines [][]byte, startIdx int) bool {
	for i := startIdx - 1; i >= 0; i-- {
		trimmed := bytes.TrimSpace(lines[i])
		if len(trimmed) == 0 {
			continue
		}
		return bytes.HasSuffix(trimmed, []byte("\\"))
	}
	return false
}
