package rules

import (
	"encoding/json"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

func TestNewFileLocation(t *testing.T) {
	loc := NewFileLocation("Dockerfile")

	if loc.File != "Dockerfile" {
		t.Errorf("File = %q, want %q", loc.File, "Dockerfile")
	}
	if loc.Start.Line != -1 {
		t.Errorf("Start.Line = %d, want -1 (file-level sentinel)", loc.Start.Line)
	}
	if !loc.IsFileLevel() {
		t.Error("IsFileLevel() = false, want true")
	}
}

func TestNewLineLocation(t *testing.T) {
	// 0-based: line 10 means the 11th line
	loc := NewLineLocation("Dockerfile", 10)

	if loc.File != "Dockerfile" {
		t.Errorf("File = %q, want %q", loc.File, "Dockerfile")
	}
	if loc.Start.Line != 10 {
		t.Errorf("Start.Line = %d, want 10", loc.Start.Line)
	}
	if loc.Start.Column != 0 {
		t.Errorf("Start.Column = %d, want 0", loc.Start.Column)
	}
	if loc.End.Line != -1 {
		t.Errorf("End.Line = %d, want -1 (point location sentinel)", loc.End.Line)
	}
	if loc.IsFileLevel() {
		t.Error("IsFileLevel() = true, want false")
	}
	if !loc.IsPointLocation() {
		t.Error("IsPointLocation() = false, want true")
	}
}

func TestNewRangeLocation(t *testing.T) {
	// 0-based coordinates
	loc := NewRangeLocation("Dockerfile", 5, 3, 7, 10)

	if loc.Start.Line != 5 {
		t.Errorf("Start.Line = %d, want 5", loc.Start.Line)
	}
	if loc.Start.Column != 3 {
		t.Errorf("Start.Column = %d, want 3", loc.Start.Column)
	}
	if loc.End.Line != 7 {
		t.Errorf("End.Line = %d, want 7", loc.End.Line)
	}
	if loc.End.Column != 10 {
		t.Errorf("End.Column = %d, want 10", loc.End.Column)
	}
	if loc.IsPointLocation() {
		t.Error("IsPointLocation() = true, want false")
	}
	if loc.IsFileLevel() {
		t.Error("IsFileLevel() = true, want false")
	}
}

func TestLocation_JSON(t *testing.T) {
	loc := NewRangeLocation("test.dockerfile", 1, 5, 3, 20)

	data, err := json.Marshal(loc)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed Location
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed.File != loc.File {
		t.Errorf("File = %q, want %q", parsed.File, loc.File)
	}
	if parsed.Start.Line != loc.Start.Line {
		t.Errorf("Start.Line = %d, want %d", parsed.Start.Line, loc.Start.Line)
	}
}

func TestNewLocationFromRange(t *testing.T) {
	// Both BuildKit and our Location use 0-based coordinates (LSP semantics)
	r := parser.Range{
		Start: parser.Position{Line: 5, Character: 10},
		End:   parser.Position{Line: 7, Character: 25},
	}

	loc := NewLocationFromRange("Dockerfile", r)

	if loc.File != "Dockerfile" {
		t.Errorf("File = %q, want %q", loc.File, "Dockerfile")
	}
	// Direct mapping, no conversion needed
	if loc.Start.Line != 5 {
		t.Errorf("Start.Line = %d, want 5", loc.Start.Line)
	}
	if loc.Start.Column != 10 {
		t.Errorf("Start.Column = %d, want 10", loc.Start.Column)
	}
	if loc.End.Line != 7 {
		t.Errorf("End.Line = %d, want 7", loc.End.Line)
	}
	if loc.End.Column != 25 {
		t.Errorf("End.Column = %d, want 25", loc.End.Column)
	}
}
