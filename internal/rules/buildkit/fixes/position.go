package fixes

import (
	"bytes"
	"strings"
	"unicode"

	"github.com/tinovyatkin/tally/internal/rules"
)

// getLine extracts a single line from source (0-based line index).
// Returns empty slice if line is out of bounds.
// Handles both LF and CRLF line endings.
func getLine(source []byte, lineIndex int) []byte {
	// Detect line ending style
	lineEnding := []byte("\n")
	if bytes.Contains(source, []byte("\r\n")) {
		lineEnding = []byte("\r\n")
	}

	lines := bytes.Split(source, lineEnding)
	if lineIndex < 0 || lineIndex >= len(lines) {
		return nil
	}
	return lines[lineIndex]
}

// isIdentChar returns true if ch is a valid identifier character.
// Docker stage names allow: [a-z][a-z0-9-_.]* per BuildKit validation.
func isIdentChar(ch byte) bool {
	return ch == '_' || ch == '-' || ch == '.' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

// findASKeyword locates the AS keyword in a FROM line.
// Returns (asStart, asEnd, nameStart, nameEnd) as byte offsets in the line.
// Returns (-1, -1, -1, -1) if AS keyword not found.
//
// Deprecated: Use ParseInstruction(line).FindKeyword("AS") and TokenAfter() instead.
// The tokenizer provides better handling of quoted values and edge cases.
func findASKeyword(line []byte) (int, int, int, int) {
	lineStr := string(line)
	lineUpper := strings.ToUpper(lineStr)

	// Find " AS " with word boundaries - use LastIndex to handle edge case
	// where image name is "as" (e.g., "FROM as AS builder")
	idx := strings.LastIndex(lineUpper, " AS ")
	asIdx := -1
	if idx != -1 {
		// AS should be preceded by whitespace (already checked) and followed by whitespace/identifier
		asIdx = idx + 1 // Skip the leading space
	}

	if asIdx == -1 {
		// Try end of line: " AS" at the very end (edge case)
		if strings.HasSuffix(lineUpper, " AS") {
			asIdx = len(lineStr) - 2
		}
	}

	if asIdx == -1 {
		return -1, -1, -1, -1
	}

	asStart := asIdx
	asEnd := asIdx + 2 // "AS" is 2 chars

	// Find the stage name after AS
	nameStart := asEnd
	// Skip whitespace
	for nameStart < len(line) && unicode.IsSpace(rune(line[nameStart])) {
		nameStart++
	}

	if nameStart >= len(line) {
		// No stage name found
		return asStart, asEnd, -1, -1
	}

	// Find end of stage name
	nameEnd := nameStart
	for nameEnd < len(line) && isIdentChar(line[nameEnd]) {
		nameEnd++
	}

	return asStart, asEnd, nameStart, nameEnd
}

// findCopyFromValue locates the --from=VALUE in a COPY command line.
// Returns the start and end byte offsets of VALUE (not including --from=).
// Returns (-1, -1) if --from not found.
//
// Deprecated: Use ParseInstruction(line).FindFlag("from") and FlagValue() instead.
// The tokenizer provides better handling of quoted values.
func findCopyFromValue(line []byte) (int, int) {
	lineStr := string(line)
	lineUpper := strings.ToUpper(lineStr)

	// Find "--from=" case-insensitively
	fromIdx := strings.Index(lineUpper, "--FROM=")
	if fromIdx == -1 {
		return -1, -1
	}

	const fromFlagLen = 7 // "--from="
	valueStart := fromIdx + fromFlagLen
	if valueStart >= len(line) {
		return -1, -1
	}

	// Find end of value (up to whitespace or end of line)
	valueEnd := valueStart
	for valueEnd < len(line) && !unicode.IsSpace(rune(line[valueEnd])) {
		valueEnd++
	}

	return valueStart, valueEnd
}

// findFROMBaseName locates the base image name in a FROM line (before AS if present).
// Returns the start and end byte offsets of the base image name.
//
// Deprecated: Use ParseInstruction(line).Arguments()[0] instead.
// The tokenizer provides better handling of edge cases.
func findFROMBaseName(line []byte) (int, int) {
	lineStr := string(line)
	lineUpper := strings.ToUpper(lineStr)

	// Skip "FROM " prefix
	if !strings.HasPrefix(lineUpper, "FROM ") {
		return -1, -1
	}

	start := 5 // len("FROM ")

	// Skip whitespace
	for start < len(line) && unicode.IsSpace(rune(line[start])) {
		start++
	}

	// Skip --platform=... if present
	if start+11 <= len(line) && strings.HasPrefix(lineUpper[start:], "--PLATFORM=") {
		// Find end of platform value
		start += 11
		for start < len(line) && !unicode.IsSpace(rune(line[start])) {
			start++
		}
		// Skip whitespace after platform
		for start < len(line) && unicode.IsSpace(rune(line[start])) {
			start++
		}
	}

	if start >= len(line) {
		return -1, -1
	}

	// Find end of base name (up to whitespace which could be before AS)
	end := start
	for end < len(line) && !unicode.IsSpace(rune(line[end])) {
		end++
	}

	return start, end
}

// createEditLocation creates a Location for an edit within a specific line.
// lineNum is 1-based (BuildKit convention), startCol and endCol are 0-based byte offsets.
// The returned Location uses 1-based line numbers consistent with BuildKit and our schema.
func createEditLocation(file string, lineNum, startCol, endCol int) rules.Location {
	return rules.NewRangeLocation(file, lineNum, startCol, lineNum, endCol)
}
