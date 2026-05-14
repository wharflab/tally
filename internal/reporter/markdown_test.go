package reporter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wharflab/tally/internal/rules"
)

func TestMarkdownReporterSingleFile(t *testing.T) {
	t.Parallel()
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 5, Column: 0},
			},
			RuleCode: "StageNameCasing",
			Message:  "Stage name 'Builder' should be lowercase",
			Severity: rules.SeverityWarning,
		},
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 10, Column: 0},
			},
			RuleCode: "DL3000",
			Message:  "Use absolute WORKDIR",
			Severity: rules.SeverityError,
		},
	}

	var buf bytes.Buffer
	reporter := NewMarkdownReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()

	// Check summary
	if !strings.Contains(output, "**2 issues** in `Dockerfile`") {
		t.Errorf("Expected summary line, got: %s", output)
	}

	// Check table headers (single file format - no File column)
	if !strings.Contains(output, "| Line | Issue |") {
		t.Errorf("Expected table header, got: %s", output)
	}

	// Check earlier line comes first even when a later issue is more severe.
	lines := strings.Split(output, "\n")
	errorLine := -1
	warningLine := -1
	for i, line := range lines {
		if strings.Contains(line, "Use absolute WORKDIR") {
			errorLine = i
		}
		if strings.Contains(line, "Stage name") {
			warningLine = i
		}
	}
	if errorLine == -1 || warningLine == -1 {
		t.Fatalf(
			"expected both error and warning lines to be present; got errorLine=%d warningLine=%d",
			errorLine,
			warningLine,
		)
	}
	if warningLine >= errorLine {
		t.Error("Expected lower line number to come before later error in output")
	}

	// Check emoji indicators
	if !strings.Contains(output, "❌") {
		t.Error("Expected error emoji (❌) in output")
	}
	if !strings.Contains(output, "⚠️") {
		t.Error("Expected warning emoji (⚠️) in output")
	}
}

func TestMarkdownReporterMultipleFiles(t *testing.T) {
	t.Parallel()
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "Dockerfile.prod",
				Start: rules.Position{Line: 5, Column: 0},
			},
			RuleCode: "TEST",
			Message:  "Issue in prod",
			Severity: rules.SeverityWarning,
		},
		{
			Location: rules.Location{
				File:  "Dockerfile.dev",
				Start: rules.Position{Line: 3, Column: 0},
			},
			RuleCode: "TEST",
			Message:  "Issue in dev",
			Severity: rules.SeverityWarning,
		},
	}

	var buf bytes.Buffer
	reporter := NewMarkdownReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()

	// Check summary mentions multiple files
	if !strings.Contains(output, "across 2 files") {
		t.Errorf("Expected multi-file summary, got: %s", output)
	}

	// Check table has File column
	if !strings.Contains(output, "| File | Line | Issue |") {
		t.Errorf("Expected multi-file table header, got: %s", output)
	}
}

func TestMarkdownReporterEmpty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	reporter := NewMarkdownReporter(&buf)

	err := reporter.Report(nil, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "**No issues found**") {
		t.Errorf("Expected no issues message, got: %s", output)
	}
}

func TestMarkdownReporterSeverityEmojis(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		severity rules.Severity
		emoji    string
	}{
		{"error", rules.SeverityError, "❌"},
		{"warning", rules.SeverityWarning, "⚠️"},
		{"info", rules.SeverityInfo, "ℹ️"},
		{"style", rules.SeverityStyle, "💅"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := severityEmoji(tt.severity)
			if result != tt.emoji {
				t.Errorf("severityEmoji(%v) = %q, want %q", tt.severity, result, tt.emoji)
			}
		})
	}
}

func TestMarkdownReporterEscaping(t *testing.T) {
	t.Parallel()
	violations := []rules.Violation{
		{
			Location: rules.Location{
				File:  "Dockerfile",
				Start: rules.Position{Line: 1, Column: 0},
			},
			RuleCode: "TEST",
			Message:  "Message with | pipe and\nnewline",
			Severity: rules.SeverityWarning,
		},
	}

	var buf bytes.Buffer
	reporter := NewMarkdownReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()

	// Pipe should be escaped
	if strings.Contains(output, "with | pipe") {
		t.Error("Expected pipe to be escaped")
	}
	if !strings.Contains(output, "with \\| pipe") {
		t.Errorf("Expected escaped pipe in output: %s", output)
	}

	// Newline should be replaced
	if strings.Contains(output, "and\nnewline") {
		t.Error("Expected newline to be removed from message")
	}
}

func TestMarkdownReporterFileLevelViolation(t *testing.T) {
	t.Parallel()
	violations := []rules.Violation{
		{
			Location: rules.NewFileLocation("Dockerfile"),
			RuleCode: "TEST",
			Message:  "File-level issue",
			Severity: rules.SeverityWarning,
		},
	}

	var buf bytes.Buffer
	reporter := NewMarkdownReporter(&buf)

	err := reporter.Report(violations, nil, ReportMetadata{})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()

	// File-level violations should show "-" for line
	if !strings.Contains(output, "| - |") {
		t.Errorf("Expected '-' for file-level violation line, got: %s", output)
	}
}

func TestMarkdownReporterSortsByLocation(t *testing.T) {
	t.Parallel()
	violations := []rules.Violation{
		{
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 20}},
			Message:  "later error",
			Severity: rules.SeverityError,
		},
		{
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 5}},
			Message:  "earlier warning",
			Severity: rules.SeverityWarning,
		},
		{
			Location: rules.Location{File: "Dockerfile", Start: rules.Position{Line: 12}},
			Message:  "middle info",
			Severity: rules.SeverityInfo,
		},
	}

	var buf bytes.Buffer
	reporter := NewMarkdownReporter(&buf)
	if err := reporter.Report(violations, nil, ReportMetadata{}); err != nil {
		t.Fatalf("Report() error = %v", err)
	}

	output := buf.String()
	earlier := strings.Index(output, "earlier warning")
	middle := strings.Index(output, "middle info")
	later := strings.Index(output, "later error")
	if earlier == -1 || middle == -1 || later == -1 {
		t.Fatalf("expected all messages in output, got: %s", output)
	}
	if earlier >= middle || middle >= later {
		t.Fatalf("expected markdown output sorted by line number, got: %s", output)
	}
}

func TestParseFormatMarkdown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected Format
		wantErr  bool
	}{
		{"markdown", FormatMarkdown, false},
		{"md", FormatMarkdown, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			format, err := ParseFormat(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFormat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && format != tt.expected {
				t.Errorf("ParseFormat(%q) = %v, want %v", tt.input, format, tt.expected)
			}
		})
	}
}
