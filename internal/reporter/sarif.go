package reporter

import (
	"io"
	"path/filepath"
	"sort"

	"github.com/owenrumney/go-sarif/v3/pkg/report/v210/sarif"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Default SARIF tool information.
const (
	defaultToolName = "tally"
	defaultToolURI  = "https://github.com/tinovyatkin/tally"
)

// SARIFReporter formats violations as SARIF (Static Analysis Results Interchange Format).
// SARIF is a standard format for static analysis tools, widely supported by CI/CD systems
// including GitHub Code Scanning and Azure DevOps.
//
// See: https://docs.oasis-open.org/sarif/sarif/v2.1.0/
type SARIFReporter struct {
	writer      io.Writer
	toolName    string
	toolVersion string
	toolURI     string
}

// NewSARIFReporter creates a new SARIF reporter.
func NewSARIFReporter(w io.Writer, toolName, toolVersion, toolURI string) *SARIFReporter {
	if toolName == "" {
		toolName = defaultToolName
	}
	if toolURI == "" {
		toolURI = defaultToolURI
	}
	return &SARIFReporter{
		writer:      w,
		toolName:    toolName,
		toolVersion: toolVersion,
		toolURI:     toolURI,
	}
}

// Report implements Reporter.
func (r *SARIFReporter) Report(violations []rules.Violation, _ map[string][]byte, _ ReportMetadata) error {
	// Create a new SARIF report (v2.1.0 for maximum compatibility)
	report := sarif.NewReport()

	// Create a run with tool information
	run := sarif.NewRunWithInformationURI(r.toolName, r.toolURI)
	if r.toolVersion != "" {
		run.Tool.Driver.WithVersion(r.toolVersion)
	}

	// Collect unique rule codes and files
	ruleSet := make(map[string]rules.Violation)
	fileSet := make(map[string]struct{})

	for _, v := range violations {
		if _, exists := ruleSet[v.RuleCode]; !exists {
			ruleSet[v.RuleCode] = v
		}
		// Normalize path for SARIF URIs (cross-platform consistency)
		filePath := filepath.ToSlash(v.Location.File)
		fileSet[filePath] = struct{}{}
	}

	// Add rule definitions
	ruleCodes := make([]string, 0, len(ruleSet))
	for code := range ruleSet {
		ruleCodes = append(ruleCodes, code)
	}
	sort.Strings(ruleCodes)

	for _, code := range ruleCodes {
		v := ruleSet[code]
		rule := run.AddRule(code)
		if v.Detail != "" {
			rule.WithShortDescription(sarif.NewMultiformatMessageString().WithText(v.Detail))
		}
		if v.DocURL != "" {
			rule.WithHelpURI(v.DocURL)
		}
	}

	// Add artifacts (files)
	files := make([]string, 0, len(fileSet))
	for file := range fileSet {
		files = append(files, file)
	}
	sort.Strings(files)

	for _, file := range files {
		run.AddDistinctArtifact(file)
	}

	// Add results
	for _, v := range violations {
		// Normalize file path (must do in each loop since range copies values)
		filePath := filepath.ToSlash(v.Location.File)

		result := sarif.NewRuleResult(v.RuleCode).
			WithMessage(sarif.NewTextMessage(v.Message)).
			WithLevel(severityToSARIFLevel(v.Severity))

		// Add location if not file-level
		if !v.Location.IsFileLevel() {
			region := sarif.NewRegion().
				WithStartLine(v.Location.Start.Line)

			// Add column if available
			if v.Location.Start.Column >= 0 {
				region.WithStartColumn(v.Location.Start.Column + 1) // SARIF uses 1-based columns
			}

			// Add end position if it's a range
			if !v.Location.IsPointLocation() && v.Location.End.Line > 0 {
				region.WithEndLine(v.Location.End.Line)
				if v.Location.End.Column >= 0 {
					region.WithEndColumn(v.Location.End.Column + 1)
				}
			}

			// Add source snippet if available
			if v.SourceCode != "" {
				region.WithSnippet(sarif.NewArtifactContent().WithText(v.SourceCode))
			}

			physicalLocation := sarif.NewPhysicalLocation().
				WithArtifactLocation(sarif.NewSimpleArtifactLocation(filePath)).
				WithRegion(region)

			result.WithLocations([]*sarif.Location{
				sarif.NewLocationWithPhysicalLocation(physicalLocation),
			})
		} else {
			// File-level violation - just include the file
			physicalLocation := sarif.NewPhysicalLocation().
				WithArtifactLocation(sarif.NewSimpleArtifactLocation(filePath))

			result.WithLocations([]*sarif.Location{
				sarif.NewLocationWithPhysicalLocation(physicalLocation),
			})
		}

		run.AddResult(result)
	}

	report.AddRun(run)

	// Write with pretty formatting for readability
	return report.PrettyWrite(r.writer)
}

// SARIF severity levels.
const (
	sarifLevelError   = "error"
	sarifLevelWarning = "warning"
	sarifLevelNote    = "note"
)

// severityToSARIFLevel maps our Severity to SARIF levels.
// SARIF uses: "error", "warning", "note", "none"
func severityToSARIFLevel(s rules.Severity) string {
	switch s {
	case rules.SeverityError:
		return sarifLevelError
	case rules.SeverityWarning:
		return sarifLevelWarning
	case rules.SeverityInfo, rules.SeverityStyle:
		return sarifLevelNote
	case rules.SeverityOff:
		// Should never reach here - filtered by EnableFilter
		return sarifLevelNote
	default:
		return sarifLevelWarning
	}
}
