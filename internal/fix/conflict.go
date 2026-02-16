package fix

import "github.com/wharflab/tally/internal/rules"

// editsOverlap checks if two edits overlap in their locations.
// Overlapping edits cannot both be applied safely.
func editsOverlap(a, b rules.TextEdit) bool {
	// Different files never overlap
	if a.Location.File != b.Location.File {
		return false
	}

	// Check if ranges overlap
	// Edit A: [a.Start, a.End)
	// Edit B: [b.Start, b.End)
	// They overlap if neither is completely before the other

	aStart := a.Location.Start
	aEnd := a.Location.End
	bStart := b.Location.Start
	bEnd := b.Location.End

	// A is completely before B
	if aEnd.Line < bStart.Line ||
		(aEnd.Line == bStart.Line && aEnd.Column <= bStart.Column) {
		return false
	}

	// B is completely before A
	if bEnd.Line < aStart.Line ||
		(bEnd.Line == aStart.Line && bEnd.Column <= aStart.Column) {
		return false
	}

	return true
}

// editPosition returns a comparable position for sorting edits.
// Returns (line, column) for the start of the edit.
func editPosition(e rules.TextEdit) (int, int) {
	return e.Location.Start.Line, e.Location.Start.Column
}

// compareEdits returns true if edit a comes before edit b in the file.
func compareEdits(a, b rules.TextEdit) bool {
	aLine, aCol := editPosition(a)
	bLine, bCol := editPosition(b)
	if aLine != bLine {
		return aLine < bLine
	}
	return aCol < bCol
}
