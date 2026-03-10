package core

import "unicode/utf8"

// RuneColsForByteRange converts a byte range within a line into rune columns.
func RuneColsForByteRange(line string, startByte, endByte int) (int, int) {
	startByte = ClampByteIndex(line, startByte)
	endByte = max(ClampByteIndex(line, endByte), startByte)

	startCol := utf8.RuneCountInString(line[:startByte])
	endCol := startCol + utf8.RuneCountInString(line[startByte:endByte])
	return startCol, endCol
}

// ClampByteIndex moves a byte index onto a valid rune boundary within line.
func ClampByteIndex(line string, idx int) int {
	if idx <= 0 {
		return 0
	}
	if idx >= len(line) {
		return len(line)
	}
	for idx > 0 && !utf8.RuneStart(line[idx]) {
		idx--
	}
	return idx
}
