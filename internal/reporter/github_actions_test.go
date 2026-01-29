package reporter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tinovyatkin/tally/internal/rules"
)

func TestGitHubActionsReporter(t *testing.T) {
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
		},
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 10, Column: 4},
				End:   rules.Position{Line: 12, Column: 0},
			},
			RuleCode: "DL3000",
			Message:  "Use absolute WORKDIR",
			Severity: rules.SeverityError,
		},
	}

	var buf bytes.Buffer
	reporter := NewGitHubActionsReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	if len(lines) != 2 {
		t.Fatalf("Expected 2 lines, got %d: %q", len(lines), output)
	}

	// Check first line (warning)
	if !strings.HasPrefix(lines[0], "::warning ") {
		t.Errorf("Expected first line to be warning, got: %s", lines[0])
	}
	if !strings.Contains(lines[0], "file=Dockerfile") {
		t.Errorf("Expected file=Dockerfile in: %s", lines[0])
	}
	if !strings.Contains(lines[0], "line=5") {
		t.Errorf("Expected line=5 in: %s", lines[0])
	}
	if !strings.Contains(lines[0], "col=1") {
		t.Errorf("Expected col=1 (column 0 becomes 1-based) in: %s", lines[0])
	}
	if !strings.Contains(lines[0], "title=DL3006") {
		t.Errorf("Expected title=DL3006 in: %s", lines[0])
	}

	// Check second line (error)
	if !strings.HasPrefix(lines[1], "::error ") {
		t.Errorf("Expected second line to be error, got: %s", lines[1])
	}
	if !strings.Contains(lines[1], "col=5") {
		t.Errorf("Expected col=5 (1-based) in: %s", lines[1])
	}
	if !strings.Contains(lines[1], "endLine=12") {
		t.Errorf("Expected endLine=12 in: %s", lines[1])
	}
}

func TestGitHubActionsReporterSeverityMapping(t *testing.T) {
	tests := []struct {
		name     string
		severity rules.Severity
		expected string
	}{
		{"error", rules.SeverityError, "error"},
		{"warning", rules.SeverityWarning, "warning"},
		{"info", rules.SeverityInfo, "notice"},
		{"style", rules.SeverityStyle, "notice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := severityToGitHubLevel(tt.severity)
			if result != tt.expected {
				t.Errorf("severityToGitHubLevel(%v) = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestGitHubActionsReporterEmpty(t *testing.T) {
	var buf bytes.Buffer
	reporter := NewGitHubActionsReporter(&buf)

	err := reporter.Report(nil, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("Expected empty output, got: %q", buf.String())
	}
}

func TestGitHubActionsReporterMessageEscaping(t *testing.T) {
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 1, Column: 0},
			},
			RuleCode: "TEST",
			Message:  "Line 1\nLine 2\r\nLine 3",
			Severity: rules.SeverityWarning,
		},
	}

	var buf bytes.Buffer
	reporter := NewGitHubActionsReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()

	// The output should be a single line (except the final newline)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 1 {
		t.Errorf("Expected single line output, got %d lines: %q", len(lines), output)
	}

	if !strings.Contains(output, "%0A") {
		t.Errorf("Expected %%0A (escaped newline) in: %s", output)
	}
}

func TestGitHubActionsReporterPropertyEscaping(t *testing.T) {
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "path/to:file,with:special.Dockerfile",
				Start: rules.Position{Line: 1, Column: 0},
			},
			RuleCode: "RULE:WITH,SPECIAL",
			Message:  "Message with : and , should NOT be escaped",
			Severity: rules.SeverityWarning,
		},
	}

	var buf bytes.Buffer
	reporter := NewGitHubActionsReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()

	// File path should have : and , escaped
	if !strings.Contains(output, "file=path/to%3Afile%2Cwith%3Aspecial.Dockerfile") {
		t.Errorf("Expected escaped file path, got: %s", output)
	}

	// Title (rule code) should have : and , escaped
	if !strings.Contains(output, "title=RULE%3AWITH%2CSPECIAL") {
		t.Errorf("Expected escaped title, got: %s", output)
	}

	// Message should NOT have : and , escaped (only in properties)
	if !strings.Contains(output, "::Message with : and , should NOT be escaped") {
		t.Errorf("Message should not escape : or , - got: %s", output)
	}
}

func TestGitHubActionsReporterSorting(t *testing.T) {
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "b.Dockerfile",
				Start: rules.Position{Line: 10, Column: 0},
			},
			RuleCode: "TEST",
			Message:  "B line 10",
			Severity: rules.SeverityWarning,
		},
		{
			Location: rules.Location{
				File:  "a.Dockerfile",
				Start: rules.Position{Line: 5, Column: 0},
			},
			RuleCode: "TEST",
			Message:  "A line 5",
			Severity: rules.SeverityWarning,
		},
		{
			Location: rules.Location{
				File:  "a.Dockerfile",
				Start: rules.Position{Line: 1, Column: 0},
			},
			RuleCode: "TEST",
			Message:  "A line 1",
			Severity: rules.SeverityWarning,
		},
	}

	var buf bytes.Buffer
	reporter := NewGitHubActionsReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("Expected 3 lines, got %d: %q", len(lines), buf.String())
	}

	// Should be sorted: a.Dockerfile:1, a.Dockerfile:5, b.Dockerfile:10
	if !strings.Contains(lines[0], "a.Dockerfile") || !strings.Contains(lines[0], "line=1") {
		t.Errorf("First line should be a.Dockerfile:1, got: %s", lines[0])
	}
	if !strings.Contains(lines[1], "a.Dockerfile") || !strings.Contains(lines[1], "line=5") {
		t.Errorf("Second line should be a.Dockerfile:5, got: %s", lines[1])
	}
	if !strings.Contains(lines[2], "b.Dockerfile") || !strings.Contains(lines[2], "line=10") {
		t.Errorf("Third line should be b.Dockerfile:10, got: %s", lines[2])
	}
}
