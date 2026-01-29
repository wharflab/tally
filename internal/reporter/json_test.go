package reporter

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestJSONReporter(t *testing.T) {
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 5, Column: 0},
				End:   rules.Position{Line: 5, Column: 20},
			},
			RuleCode: "DL3006",
			Message:  "Always tag the version of an image explicitly",
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
	reporter := NewJSONReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	// Parse the output
	var output JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Verify structure
	if len(output.Files) != 1 {
		t.Errorf("Expected 1 file, got %d", len(output.Files))
	}

	if output.Files[0].File != "Dockerfile" {
		t.Errorf("Expected file 'Dockerfile', got %q", output.Files[0].File)
	}

	if len(output.Files[0].Violations) != 2 {
		t.Errorf("Expected 2 violations, got %d", len(output.Files[0].Violations))
	}

	// Verify summary
	if output.Summary.Total != 2 {
		t.Errorf("Expected total 2, got %d", output.Summary.Total)
	}

	if output.Summary.Errors != 1 {
		t.Errorf("Expected 1 error, got %d", output.Summary.Errors)
	}

	if output.Summary.Warnings != 1 {
		t.Errorf("Expected 1 warning, got %d", output.Summary.Warnings)
	}
}

func TestJSONReporterMultipleFiles(t *testing.T) {
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "Dockerfile.prod",
				Start: rules.Position{Line: 1, Column: 0},
			},
			RuleCode: "DL3006",
			Message:  "Test",
			Severity: rules.SeverityWarning,
		},
		{
			Location: rules.Location{
				File:  "Dockerfile.dev",
				Start: rules.Position{Line: 1, Column: 0},
			},
			RuleCode: "DL3000",
			Message:  "Test",
			Severity: rules.SeverityError,
		},
		{
			Location: rules.Location{
				File:  "Dockerfile.prod",
				Start: rules.Position{Line: 5, Column: 0},
			},
			RuleCode: "DL3001",
			Message:  "Test",
			Severity: rules.SeverityInfo,
		},
	}

	var buf bytes.Buffer
	reporter := NewJSONReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	var output JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Should have 2 files
	if len(output.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(output.Files))
	}

	// Summary should reflect all violations
	if output.Summary.Total != 3 {
		t.Errorf("Expected total 3, got %d", output.Summary.Total)
	}

	if output.Summary.Files != 2 {
		t.Errorf("Expected 2 files in summary, got %d", output.Summary.Files)
	}
}

func TestJSONReporterEmpty(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewJSONReporter(&buf)

	err := reporter.Report(nil, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	var output JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Should have empty files array, not null
	if output.Files == nil {
		t.Error("Expected empty array, got nil")
	}

	if output.Summary.Total != 0 {
		t.Errorf("Expected total 0, got %d", output.Summary.Total)
	}
}
