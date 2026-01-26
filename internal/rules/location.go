package rules

import "github.com/moby/buildkit/frontend/dockerfile/parser"

// Position represents a single point in a source file.
// This is our JSON-serializable equivalent of parser.Position, which uses
// "Character" instead of "Column" and lacks JSON tags.
//
// We use 0-based coordinates to align with BuildKit (LSP semantics).
//
// See: github.com/moby/buildkit/frontend/dockerfile/parser.Position
type Position struct {
	// Line is the 0-based line number (LSP semantics, same as BuildKit).
	Line int `json:"line"`
	// Column is the 0-based column number (LSP semantics, same as BuildKit).
	// Note: BuildKit uses "Character" for this field.
	Column int `json:"column,omitempty"`
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
	// Start is the starting position (inclusive, 0-based).
	Start Position `json:"start"`
	// End is the ending position (exclusive, LSP semantics).
	// Points to the first position after the range. If negative, it's a point location.
	End Position `json:"end"`
}

// NewFileLocation creates a location for file-level issues (no specific line).
// Uses -1 as sentinel since 0 is a valid line number in 0-based coordinates.
func NewFileLocation(file string) Location {
	return Location{
		File:  file,
		Start: Position{Line: -1, Column: -1},
		End:   Position{Line: -1, Column: -1},
	}
}

// NewLineLocation creates a location for a specific line (0-based).
// Creates a point location (no range) at the start of the line.
func NewLineLocation(file string, line int) Location {
	return Location{
		File:  file,
		Start: Position{Line: line, Column: 0},
		End:   Position{Line: -1, Column: -1}, // Point location sentinel
	}
}

// NewRangeLocation creates a location spanning multiple lines/columns (0-based).
func NewRangeLocation(file string, startLine, startCol, endLine, endCol int) Location {
	return Location{
		File:  file,
		Start: Position{Line: startLine, Column: startCol},
		End:   Position{Line: endLine, Column: endCol},
	}
}

// NewLocationFromRange converts a BuildKit parser.Range to our Location type.
// This bridges BuildKit's internal types with our output schema.
// Both use 0-based coordinates with End-exclusive semantics (LSP conventions).
// The mapping is direct—no coordinate adjustment needed.
func NewLocationFromRange(file string, r parser.Range) Location {
	return Location{
		File:  file,
		Start: Position{Line: r.Start.Line, Column: r.Start.Character},
		End:   Position{Line: r.End.Line, Column: r.End.Character},
	}
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
