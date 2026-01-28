package rules

import "github.com/moby/buildkit/frontend/dockerfile/parser"

// Position represents a single point in a source file.
// This is our JSON-serializable equivalent of parser.Position, which uses
// "Character" instead of "Column" and lacks JSON tags.
//
// We use 1-based line numbers to align with BuildKit's internal representation.
// Note: BuildKit's AST uses 1-based lines (first line is 1), not 0-based.
//
// See: github.com/moby/buildkit/frontend/dockerfile/parser.Position
type Position struct {
	// Line is the 1-based line number (first line is 1, same as BuildKit).
	Line int `json:"line"`
	// Column is the 0-based column number (same as BuildKit's Character field).
	Column int `json:"column"`
}

// Location represents a range in a source file.
// This extends parser.Range by adding the File path. BuildKit's Range only
// contains Start/End positions without file context.
//
// Following LSP conventions, Start is inclusive and End is exclusive.
// This means End points to the first position AFTER the covered text.
//
// See: github.com/moby/buildkit/frontend/dockerfile/parser.Range
type Location struct {
	// File is the path to the source file (not in parser.Range).
	File string `json:"file"`
	// Start is the starting position (inclusive, 1-based line numbers).
	Start Position `json:"start"`
	// End is the ending position (exclusive, LSP semantics).
	// Points to the first position after the range.
	// A point location has End.Line < 0 (unset) or End equals Start.
	End Position `json:"end"`
}

// NewFileLocation creates a location for file-level issues (no specific line).
// Uses -1 as sentinel since 0 would be invalid (lines are 1-based).
func NewFileLocation(file string) Location {
	return Location{
		File:  file,
		Start: Position{Line: -1, Column: -1},
		End:   Position{Line: -1, Column: -1},
	}
}

// NewLineLocation creates a location for a specific line (1-based).
// Creates a point location (no range) at the start of the line.
func NewLineLocation(file string, line int) Location {
	return Location{
		File:  file,
		Start: Position{Line: line, Column: 0},
		End:   Position{Line: -1, Column: -1}, // Point location sentinel
	}
}

// NewRangeLocation creates a location spanning multiple lines/columns.
// Lines are 1-based, columns are 0-based.
func NewRangeLocation(file string, startLine, startCol, endLine, endCol int) Location {
	return Location{
		File:  file,
		Start: Position{Line: startLine, Column: startCol},
		End:   Position{Line: endLine, Column: endCol},
	}
}

// NewLocationFromRange converts a BuildKit parser.Range to our Location type.
// This bridges BuildKit's internal types with our output schema.
// BuildKit uses 1-based line numbers. The mapping is direct—no adjustment needed.
func NewLocationFromRange(file string, r parser.Range) Location {
	return Location{
		File:  file,
		Start: Position{Line: r.Start.Line, Column: r.Start.Character},
		End:   Position{Line: r.End.Line, Column: r.End.Character},
	}
}

// NewLocationFromRanges creates a Location from a slice of BuildKit Ranges.
// Uses the first range if multiple exist, or returns file-level if empty.
func NewLocationFromRanges(file string, ranges []parser.Range) Location {
	if len(ranges) == 0 {
		return NewFileLocation(file)
	}
	return NewLocationFromRange(file, ranges[0])
}

// IsFileLevel returns true if this is a file-level location (no specific line).
func (l Location) IsFileLevel() bool {
	return l.Start.Line < 0
}

// IsPointLocation returns true if this is a single-point location (no range).
// A point location has End.Line < 0 (unset) or End equals Start.
func (l Location) IsPointLocation() bool {
	return l.End.Line < 0 || (l.End.Line == l.Start.Line && l.End.Column == l.Start.Column)
}
