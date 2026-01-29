package reporter

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
)

// GitHubActionsReporter formats violations as GitHub Actions workflow commands.
// These commands appear as annotations in the GitHub Actions UI.
//
// Format: ::{level} file={file},line={line},col={col}::{message}
//
// See: https://docs.github.com/actions/using-workflows/workflow-commands-for-github-actions#setting-an-error-message
type GitHubActionsReporter struct {
	writer io.Writer
}

// NewGitHubActionsReporter creates a new GitHub Actions reporter.
func NewGitHubActionsReporter(w io.Writer) *GitHubActionsReporter {
	return &GitHubActionsReporter{writer: w}
}

// Report implements Reporter.
func (r *GitHubActionsReporter) Report(violations []rules.Violation, _ map[string][]byte, _ ReportMetadata) error {
	sorted := SortViolations(violations)

	for _, v := range sorted {
		level := severityToGitHubLevel(v.Severity)

		// Normalize file path to forward slashes for consistent output
		filePath := filepath.ToSlash(v.Location.File)

		// Build the annotation
		// Format: ::{level} file={file},line={line},col={col},title={title}::{message}
		var parts []string
		parts = append(parts, "file="+escapeGitHubProperty(filePath))

		if !v.Location.IsFileLevel() {
			parts = append(parts, fmt.Sprintf("line=%d", v.Location.Start.Line))
			if v.Location.Start.Column >= 0 {
				parts = append(parts, fmt.Sprintf("col=%d", v.Location.Start.Column+1)) // 1-based
			}
			if !v.Location.IsPointLocation() && v.Location.End.Line > v.Location.Start.Line {
				parts = append(parts, fmt.Sprintf("endLine=%d", v.Location.End.Line))
			}
		}

		// Add rule code as title
		parts = append(parts, "title="+escapeGitHubProperty(v.RuleCode))

		// Escape message (newlines not allowed in workflow commands)
		message := escapeGitHubMessage(v.Message)

		if _, err := fmt.Fprintf(r.writer, "::%s %s::%s\n",
			level,
			strings.Join(parts, ","),
			message,
		); err != nil {
			return err
		}
	}

	return nil
}

// GitHub Actions annotation levels.
const (
	ghLevelError   = "error"
	ghLevelWarning = "warning"
	ghLevelNotice  = "notice"
)

// severityToGitHubLevel maps our Severity to GitHub Actions levels.
// GitHub supports: "error", "warning", "notice", "debug"
func severityToGitHubLevel(s rules.Severity) string {
	switch s {
	case rules.SeverityError:
		return ghLevelError
	case rules.SeverityWarning:
		return ghLevelWarning
	case rules.SeverityInfo, rules.SeverityStyle:
		return ghLevelNotice
	case rules.SeverityOff:
		// Should never reach here - filtered by EnableFilter
		return ghLevelWarning
	default:
		return ghLevelWarning
	}
}

// escapeGitHubMessage escapes special characters in GitHub Actions workflow command messages.
// Messages use escapeData() rules which escape "%", "\r", "\n" but NOT ":" or ",".
// See: https://github.com/actions/toolkit/blob/main/packages/core/src/command.ts
func escapeGitHubMessage(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}

// escapeGitHubProperty escapes special characters in GitHub Actions workflow command properties.
// Properties (file, title, etc.) use escapeProperty() rules which escape "%", "\r", "\n", ":", and ",".
// See: https://github.com/actions/toolkit/blob/main/packages/core/src/command.ts
func escapeGitHubProperty(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, ",", "%2C")
	return s
}
