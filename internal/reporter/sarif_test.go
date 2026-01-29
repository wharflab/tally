package reporter

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestSARIFReporter(t *testing.T) {
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 5, Column: 0},
				End:   rules.Position{Line: 5, Column: 20},
			},
			RuleCode: "DL3006",
			Message:  "Always tag the version of an image explicitly",
			Detail:   "Use explicit version tags",
			Severity: rules.SeverityWarning,
			DocURL:   "https://docs.tally.dev/rules/DL3006",
		},
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 10, Column: 0},
				End:   rules.Position{Line: 10, Column: 10},
			},
			RuleCode: "DL3000",
			Message:  "Use absolute WORKDIR",
			Severity: rules.SeverityError,
		},
	}

	var buf bytes.Buffer
	reporter := NewSARIFReporter(&buf, "tally", "1.0.0", "https://github.com/tinovyatkin/tally")

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	// Parse the SARIF output
	var sarif map[string]any
	if err := json.Unmarshal(buf.Bytes(), &sarif); err != nil {
		t.Fatalf("Failed to parse SARIF output: %v\nOutput: %s", err, buf.String())
	}

	// Verify SARIF structure
	if sarif["$schema"] == nil {
		t.Error("Missing $schema in SARIF output")
	}

	if sarif["version"] != "2.1.0" {
		t.Errorf("Expected SARIF version 2.1.0, got %v", sarif["version"])
	}

	runs, ok := sarif["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("Expected 1 run, got %v", sarif["runs"])
	}

	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected run to be map, got %T", runs[0])
	}

	// Check tool information
	tool, ok := run["tool"].(map[string]any)
	if !ok {
		t.Fatalf("Expected tool to be map, got %T", run["tool"])
	}
	driver, ok := tool["driver"].(map[string]any)
	if !ok {
		t.Fatalf("Expected driver to be map, got %T", tool["driver"])
	}

	if driver["name"] != "tally" {
		t.Errorf("Expected tool name 'tally', got %v", driver["name"])
	}

	if driver["version"] != "1.0.0" {
		t.Errorf("Expected tool version '1.0.0', got %v", driver["version"])
	}

	// Check results
	results, ok := run["results"].([]any)
	if !ok {
		t.Fatalf("Expected results to be array, got %T", run["results"])
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Check first result
	result1, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected result to be map, got %T", results[0])
	}
	if result1["ruleId"] != "DL3006" {
		t.Errorf("Expected ruleId 'DL3006', got %v", result1["ruleId"])
	}
	if result1["level"] != "warning" {
		t.Errorf("Expected level 'warning', got %v", result1["level"])
	}

	// Check second result
	result2, ok := results[1].(map[string]any)
	if !ok {
		t.Fatalf("Expected result to be map, got %T", results[1])
	}
	if result2["ruleId"] != "DL3000" {
		t.Errorf("Expected ruleId 'DL3000', got %v", result2["ruleId"])
	}
	if result2["level"] != "error" {
		t.Errorf("Expected level 'error', got %v", result2["level"])
	}
}

func TestSARIFReporterSeverityMapping(t *testing.T) {
	tests := []struct {
		severity rules.Severity
		expected string
	}{
		{rules.SeverityError, "error"},
		{rules.SeverityWarning, "warning"},
		{rules.SeverityInfo, "note"},
		{rules.SeverityStyle, "note"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := severityToSARIFLevel(tt.severity)
			if result != tt.expected {
				t.Errorf("severityToSARIFLevel(%v) = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestSARIFReporterEmpty(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewSARIFReporter(&buf, "tally", "1.0.0", "")

	err := reporter.Report(nil, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	// Should produce valid SARIF with empty results
	var sarif map[string]any
	if err := json.Unmarshal(buf.Bytes(), &sarif); err != nil {
		t.Fatalf("Failed to parse SARIF output: %v", err)
	}

	runs, ok := sarif["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("Expected 1 run, got %v", sarif["runs"])
	}

	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected run to be map, got %T", runs[0])
	}

	results, ok := run["results"].([]any)
	if !ok {
		t.Fatalf("Expected results to be array, got %T", run["results"])
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

func TestSARIFReporterColumnZero(t *testing.T) {
	// Verify that column 0 (0-based) maps to SARIF column 1 (1-based)
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 1, Column: 0},
			},
			RuleCode: "TEST",
			Message:  "Column zero test",
			Severity: rules.SeverityWarning,
		},
	}

	var buf bytes.Buffer
	reporter := NewSARIFReporter(&buf, "tally", "1.0.0", "")

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	var sarif map[string]any
	if err := json.Unmarshal(buf.Bytes(), &sarif); err != nil {
		t.Fatalf("Failed to parse SARIF output: %v", err)
	}

	runs, ok := sarif["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatal("Expected runs array in SARIF output")
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatal("Expected run to be map")
	}
	results, ok := run["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatal("Expected results array")
	}
	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatal("Expected result to be map")
	}
	locations, ok := result["locations"].([]any)
	if !ok || len(locations) == 0 {
		t.Fatal("Expected locations array")
	}
	location, ok := locations[0].(map[string]any)
	if !ok {
		t.Fatal("Expected location to be map")
	}
	physicalLocation, ok := location["physicalLocation"].(map[string]any)
	if !ok {
		t.Fatal("Expected physicalLocation to be map")
	}
	region, ok := physicalLocation["region"].(map[string]any)
	if !ok {
		t.Fatal("Expected region to be map")
	}

	// Column 0 in 0-based should become column 1 in 1-based SARIF
	startColumn, ok := region["startColumn"].(float64)
	if !ok {
		t.Fatal("Expected startColumn in region")
	}
	if startColumn != 1 {
		t.Errorf("Expected startColumn=1 (0-based column 0 maps to 1-based column 1), got %v", startColumn)
	}
}

func TestSARIFReporterFileLevelViolation(t *testing.T) {
	violations := []rules.Violation{
		{
			Location: rules.NewFileLocation("Dockerfile"),
			RuleCode: "DL3001",
			Message:  "File-level issue",
			Severity: rules.SeverityWarning,
		},
	}

	var buf bytes.Buffer
	reporter := NewSARIFReporter(&buf, "tally", "1.0.0", "")

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	// Parse and verify it doesn't include line numbers for file-level violations
	var sarif map[string]any
	if err := json.Unmarshal(buf.Bytes(), &sarif); err != nil {
		t.Fatalf("Failed to parse SARIF output: %v", err)
	}

	runs, ok := sarif["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("Expected 1 run, got %v", sarif["runs"])
	}

	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected run to be map, got %T", runs[0])
	}

	results, ok := run["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("Expected 1 result, got %v", run["results"])
	}

	result, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected result to be map, got %T", results[0])
	}

	locations, ok := result["locations"].([]any)
	if !ok || len(locations) != 1 {
		t.Fatalf("Expected 1 location, got %v", result["locations"])
	}

	location, ok := locations[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected location to be map, got %T", locations[0])
	}

	physicalLocation, ok := location["physicalLocation"].(map[string]any)
	if !ok {
		t.Fatalf("Expected physicalLocation to be map, got %T", location["physicalLocation"])
	}

	// Should have artifact location but no region for file-level
	if physicalLocation["artifactLocation"] == nil {
		t.Error("Expected artifactLocation in physical location")
	}
}
