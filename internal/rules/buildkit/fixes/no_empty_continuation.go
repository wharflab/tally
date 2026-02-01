package fixes

import (
	"bytes"

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
		// If this is the last line, span to end of this line only
		if lineIdx >= len(lines)-1 {
			lineContent := lines[lineIdx]
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(v.Location.File, lineNum, 0, lineNum, len(lineContent)),
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
func splitLines(source []byte) [][]byte {
	// Handle both LF and CRLF
	lineEnding := []byte("\n")
	if bytes.Contains(source, []byte("\r\n")) {
		lineEnding = []byte("\r\n")
	}
	return bytes.Split(source, lineEnding)
}

// findEmptyContinuationLines finds empty lines within a multi-line command.
// It scans backward from endLineIdx to find all empty lines that are part of
// the continuation sequence (lines between a line ending with '\' and the next content).
//
// endLineIdx is 0-based line index.
func findEmptyContinuationLines(lines [][]byte, endLineIdx int) []int {
	var emptyIndices []int

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
			if !isPartOfMultilineCommand(lines, i) {
				break scanLoop
			}
		}
	}

	// Reverse to get ascending order
	for i, j := 0, len(emptyIndices)-1; i < j; i, j = i+1, j-1 {
		emptyIndices[i], emptyIndices[j] = emptyIndices[j], emptyIndices[i]
	}

	return emptyIndices
}

// hasContinuationBefore checks if any preceding non-empty line ends with '\'.
func hasContinuationBefore(lines [][]byte, emptyIdx int) bool {
	for i := emptyIdx - 1; i >= 0; i-- {
		trimmed := bytes.TrimSpace(lines[i])
		if len(trimmed) == 0 {
			// Skip empty lines
			continue
		}
		// Found a non-empty line - check if it ends with '\'
		return bytes.HasSuffix(trimmed, []byte("\\"))
	}
	return false
}

// isPartOfMultilineCommand checks if a line is part of a multi-line command.
// A line is part of a multi-line command if a preceding line ends with '\'.
func isPartOfMultilineCommand(lines [][]byte, lineIdx int) bool {
	for i := lineIdx - 1; i >= 0; i-- {
		trimmed := bytes.TrimSpace(lines[i])
		if len(trimmed) == 0 {
			continue
		}
		if bytes.HasSuffix(trimmed, []byte("\\")) {
			return true
		}
		// Found a line that doesn't continue - not part of multi-line
		return false
	}
	return false
}
