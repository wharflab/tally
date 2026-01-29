package reporter

import (
	"encoding/json"
	"io"
	"path/filepath"

	"github.com/tinovyatkin/tally/internal/rules"
)

// JSONOutput is the top-level structure for JSON output.
type JSONOutput struct {
	// Files contains results grouped by file.
	Files []FileResult `json:"files"`
	// Summary contains aggregate statistics.
	Summary Summary `json:"summary"`
	// FilesScanned is the total number of files scanned.
	FilesScanned int `json:"files_scanned"`
	// RulesEnabled is the total number of rules that were active.
	RulesEnabled int `json:"rules_enabled"`
}

// FileResult contains the linting results for a single file.
type FileResult struct {
	File       string            `json:"file"`
	Violations []rules.Violation `json:"violations"`
}

// Summary contains aggregate statistics about violations.
type Summary struct {
	Total    int `json:"total"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Info     int `json:"info"`
	Style    int `json:"style"`
	Files    int `json:"files"`
}

// JSONReporter formats violations as JSON output.
type JSONReporter struct {
	writer io.Writer
}

// NewJSONReporter creates a new JSON reporter.
func NewJSONReporter(w io.Writer) *JSONReporter {
	return &JSONReporter{writer: w}
}

// Report implements Reporter.
func (r *JSONReporter) Report(violations []rules.Violation, _ map[string][]byte, metadata ReportMetadata) error {
	// Group violations by file (deterministic order)
	// Normalize paths to forward slashes for cross-platform consistency
	byFile := make(map[string][]rules.Violation)
	filesOrder := make([]string, 0)

	for _, v := range SortViolations(violations) {
		// Normalize file path in location for consistent output
		v.Location.File = filepath.ToSlash(v.Location.File)
		file := v.Location.File
		if _, exists := byFile[file]; !exists {
			filesOrder = append(filesOrder, file)
		}
		byFile[file] = append(byFile[file], v)
	}

	// Build output structure
	output := JSONOutput{
		Files:        make([]FileResult, 0, len(filesOrder)),
		Summary:      calculateSummary(violations, len(filesOrder)),
		FilesScanned: metadata.FilesScanned,
		RulesEnabled: metadata.RulesEnabled,
	}

	for _, file := range filesOrder {
		output.Files = append(output.Files, FileResult{
			File:       file,
			Violations: byFile[file],
		})
	}

	enc := json.NewEncoder(r.writer)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

// calculateSummary computes aggregate statistics from violations.
func calculateSummary(violations []rules.Violation, fileCount int) Summary {
	summary := Summary{
		Total: len(violations),
		Files: fileCount,
	}

	for _, v := range violations {
		switch v.Severity {
		case rules.SeverityError:
			summary.Errors++
		case rules.SeverityWarning:
			summary.Warnings++
		case rules.SeverityInfo:
			summary.Info++
		case rules.SeverityStyle:
			summary.Style++
		case rules.SeverityOff:
			// Should never reach here - filtered by EnableFilter
		}
	}

	return summary
}
