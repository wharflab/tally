package rules

import (
	"encoding/json"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

func TestNewViolation(t *testing.T) {
	loc := NewLineLocation("Dockerfile", 5)
	v := NewViolation(loc, "test-rule", "test message", SeverityWarning)

	if v.RuleCode != "test-rule" {
		t.Errorf("RuleCode = %q, want %q", v.RuleCode, "test-rule")
	}
	if v.Message != "test message" {
		t.Errorf("Message = %q, want %q", v.Message, "test message")
	}
	if v.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want %v", v.Severity, SeverityWarning)
	}
	if v.File() != "Dockerfile" {
		t.Errorf("File() = %q, want %q", v.File(), "Dockerfile")
	}
	if v.Line() != 5 {
		t.Errorf("Line() = %d, want 5", v.Line())
	}
}

func TestViolation_WithMethods(t *testing.T) {
	loc := NewLineLocation("Dockerfile", 1)
	v := NewViolation(loc, "rule", "msg", SeverityError).
		WithDetail("extra detail").
		WithDocURL("https://example.com/doc").
		WithSourceCode("FROM alpine")

	if v.Detail != "extra detail" {
		t.Errorf("Detail = %q, want %q", v.Detail, "extra detail")
	}
	if v.DocURL != "https://example.com/doc" {
		t.Errorf("DocURL = %q, want %q", v.DocURL, "https://example.com/doc")
	}
	if v.SourceCode != "FROM alpine" {
		t.Errorf("SourceCode = %q, want %q", v.SourceCode, "FROM alpine")
	}
}

func TestViolation_WithSuggestedFix(t *testing.T) {
	loc := NewRangeLocation("Dockerfile", 1, 1, 1, 12)
	fix := &SuggestedFix{
		Description: "Use specific tag",
		Edits: []TextEdit{
			{
				Location: loc,
				NewText:  "FROM alpine:3.18",
			},
		},
	}

	v := NewViolation(loc, "DL3006", "Always specify tag", SeverityWarning).
		WithSuggestedFix(fix)

	if v.SuggestedFix == nil {
		t.Fatal("SuggestedFix is nil")
	}
	if v.SuggestedFix.Description != "Use specific tag" {
		t.Errorf("SuggestedFix.Description = %q", v.SuggestedFix.Description)
	}
	if len(v.SuggestedFix.Edits) != 1 {
		t.Fatalf("len(Edits) = %d, want 1", len(v.SuggestedFix.Edits))
	}
	if v.SuggestedFix.Edits[0].NewText != "FROM alpine:3.18" {
		t.Errorf("Edit.NewText = %q", v.SuggestedFix.Edits[0].NewText)
	}
}

func TestViolation_JSON(t *testing.T) {
	loc := NewLineLocation("Dockerfile", 10)
	v := NewViolation(loc, "max-lines", "file too long", SeverityError).
		WithDocURL("https://example.com")

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed Violation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed.RuleCode != v.RuleCode {
		t.Errorf("RuleCode = %q, want %q", parsed.RuleCode, v.RuleCode)
	}
	if parsed.Message != v.Message {
		t.Errorf("Message = %q, want %q", parsed.Message, v.Message)
	}
	if parsed.Severity != v.Severity {
		t.Errorf("Severity = %v, want %v", parsed.Severity, v.Severity)
	}
	if parsed.Line() != v.Line() {
		t.Errorf("Line() = %d, want %d", parsed.Line(), v.Line())
	}
}

func TestViolation_JSON_WithFix(t *testing.T) {
	loc := NewLineLocation("Dockerfile", 1)
	fix := &SuggestedFix{
		Description: "Fix the issue",
		Edits: []TextEdit{
			{Location: loc, NewText: "new text"},
		},
	}
	v := NewViolation(loc, "rule", "msg", SeverityWarning).WithSuggestedFix(fix)

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var parsed Violation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if parsed.SuggestedFix == nil {
		t.Fatal("SuggestedFix is nil after unmarshal")
	}
	if parsed.SuggestedFix.Description != "Fix the issue" {
		t.Errorf("SuggestedFix.Description = %q", parsed.SuggestedFix.Description)
	}
}

func TestNewViolationFromBuildKitWarning(t *testing.T) {
	// Test with location (0-based coordinates from BuildKit)
	location := []parser.Range{
		{
			Start: parser.Position{Line: 5, Character: 1},
			End:   parser.Position{Line: 5, Character: 20},
		},
	}

	v := NewViolationFromBuildKitWarning(
		"Dockerfile",
		"StageNameCasing",
		"Stage names should be lowercase",
		"https://docs.docker.com/go/dockerfile/rule/stage-name-casing/",
		"Stage name 'Builder' should be lowercase",
		location,
	)

	if v.RuleCode != "buildkit/StageNameCasing" {
		t.Errorf("RuleCode = %q, want %q", v.RuleCode, "buildkit/StageNameCasing")
	}
	if v.Message != "Stage name 'Builder' should be lowercase" {
		t.Errorf("Message = %q", v.Message)
	}
	if v.Detail != "Stage names should be lowercase" {
		t.Errorf("Detail = %q", v.Detail)
	}
	if v.DocURL != "https://docs.docker.com/go/dockerfile/rule/stage-name-casing/" {
		t.Errorf("DocURL = %q", v.DocURL)
	}
	if v.Severity != SeverityWarning {
		t.Errorf("Severity = %v, want %v", v.Severity, SeverityWarning)
	}
	// Direct mapping, 0-based coordinates preserved
	if v.Location.Start.Line != 5 {
		t.Errorf("Location.Start.Line = %d, want 5", v.Location.Start.Line)
	}
	if v.Location.Start.Column != 1 {
		t.Errorf("Location.Start.Column = %d, want 1", v.Location.Start.Column)
	}
}

func TestNewViolationFromBuildKitWarning_NoLocation(t *testing.T) {
	// Test without location (file-level warning)
	v := NewViolationFromBuildKitWarning(
		"Dockerfile",
		"SomeRule",
		"Description",
		"https://example.com",
		"File-level warning",
		nil,
	)

	if v.Location.File != "Dockerfile" {
		t.Errorf("File = %q, want %q", v.Location.File, "Dockerfile")
	}
	if v.Location.Start.Line != -1 {
		t.Errorf("Start.Line = %d, want -1 (file-level sentinel)", v.Location.Start.Line)
	}
	if !v.Location.IsFileLevel() {
		t.Error("IsFileLevel() = false, want true")
	}
}
