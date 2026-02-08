package reporter

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
)

// MarkdownReporter formats violations as concise markdown tables.
// Designed for AI agents working on Dockerfiles - token-efficient and actionable.
type MarkdownReporter struct {
	writer io.Writer
}

// NewMarkdownReporter creates a new Markdown reporter.
func NewMarkdownReporter(w io.Writer) *MarkdownReporter {
	return &MarkdownReporter{writer: w}
}

// Report implements Reporter.
func (r *MarkdownReporter) Report(violations []rules.Violation, _ map[string][]byte, _ ReportMetadata) error {
	if len(violations) == 0 {
		_, err := fmt.Fprintln(r.writer, "**No issues found**")
		return err
	}

	sorted := SortViolationsBySeverity(violations)

	// Normalize file paths for consistent output
	for i := range sorted {
		sorted[i].Location.File = filepath.ToSlash(sorted[i].Location.File)
	}

	// Count files and issues
	fileSet := make(map[string]struct{})
	for _, v := range sorted {
		fileSet[v.Location.File] = struct{}{}
	}
	fileCount := len(fileSet)

	// Write summary and table
	if fileCount == 1 {
		var filename string
		for f := range fileSet {
			filename = f
		}
		return r.writeSingleFileTable(sorted, filename)
	}

	return r.writeMultiFileTable(sorted, fileCount)
}

// writeSingleFileTable writes a markdown table for violations in a single file.
func (r *MarkdownReporter) writeSingleFileTable(sorted []rules.Violation, filename string) error {
	if _, err := fmt.Fprintf(r.writer, "**%d %s** in `%s`\n\n",
		len(sorted), pluralize(len(sorted), "issue", "issues"), filename); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.writer, "| Line | Issue |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.writer, "|------|-------|"); err != nil {
		return err
	}

	for _, v := range sorted {
		if _, err := fmt.Fprintf(r.writer, "| %s | %s %s |\n",
			formatLineNumber(v), severityEmoji(v.Severity), escapeMarkdown(v.Message)); err != nil {
			return err
		}
	}

	return nil
}

// writeMultiFileTable writes a markdown table for violations across multiple files.
func (r *MarkdownReporter) writeMultiFileTable(sorted []rules.Violation, fileCount int) error {
	if _, err := fmt.Fprintf(r.writer, "**%d %s** across %d files\n\n",
		len(sorted), pluralize(len(sorted), "issue", "issues"), fileCount); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.writer, "| File | Line | Issue |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(r.writer, "|------|------|-------|"); err != nil {
		return err
	}

	for _, v := range sorted {
		if _, err := fmt.Fprintf(r.writer, "| %s | %s | %s %s |\n",
			v.Location.File, formatLineNumber(v), severityEmoji(v.Severity), escapeMarkdown(v.Message)); err != nil {
			return err
		}
	}

	return nil
}

// formatLineNumber returns the display string for a violation's line number.
func formatLineNumber(v rules.Violation) string {
	line := v.Location.Start.Line
	if v.Location.IsFileLevel() {
		line = 0
	}
	if line > 0 {
		return strconv.Itoa(line)
	}
	return "-"
}

// SortViolationsBySeverity sorts violations by severity (errors first), then by file and line.
// Uses stable sort to preserve original order for equal-priority items.
func SortViolationsBySeverity(violations []rules.Violation) []rules.Violation {
	sorted := make([]rules.Violation, len(violations))
	copy(sorted, violations)

	sort.SliceStable(sorted, func(i, j int) bool {
		// shouldSwap returns true if i should come AFTER j,
		// so we invert arguments to get "less than" semantics
		return shouldSwap(sorted[j], sorted[i])
	})

	return sorted
}

// shouldSwap returns true if a should come after b in the sorted output.
func shouldSwap(a, b rules.Violation) bool {
	// Sort by severity first (error < warning < info < style)
	aPriority := severityPriority(a.Severity)
	bPriority := severityPriority(b.Severity)
	if aPriority != bPriority {
		return aPriority > bPriority
	}

	// Then by file
	if a.Location.File != b.Location.File {
		return a.Location.File > b.Location.File
	}

	// Then by line
	return a.Location.Start.Line > b.Location.Start.Line
}

// severityPriority returns a numeric priority for sorting (lower = more severe).
func severityPriority(s rules.Severity) int {
	switch s {
	case rules.SeverityError:
		return 0
	case rules.SeverityWarning:
		return 1
	case rules.SeverityInfo:
		return 2
	case rules.SeverityStyle:
		return 3
	case rules.SeverityOff:
		return 5 // Should never reach here
	default:
		return 4
	}
}

// severityEmoji returns an emoji indicator for the severity level.
func severityEmoji(s rules.Severity) string {
	switch s {
	case rules.SeverityError:
		return "❌"
	case rules.SeverityWarning:
		return "⚠️"
	case rules.SeverityInfo:
		return "ℹ️"
	case rules.SeverityStyle:
		return "💅"
	case rules.SeverityOff:
		return "⭕" // Should never reach here
	default:
		return "⚠️"
	}
}

// escapeMarkdown escapes special markdown characters in table cells.
func escapeMarkdown(s string) string {
	// Escape pipe characters which break table formatting
	s = strings.ReplaceAll(s, "|", "\\|")
	// Replace newlines with spaces
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// pluralize returns singular or plural form based on count.
func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
