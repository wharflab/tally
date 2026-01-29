// Package reporter provides output formatters for lint results.
//
// The package supports multiple output formats:
//   - text: Human-readable terminal output with colors and syntax highlighting
//   - json: Machine-readable JSON output
//   - sarif: Static Analysis Results Interchange Format for CI/CD integration
//   - github-actions: Native GitHub Actions workflow annotations
//   - markdown: Concise markdown tables for AI agents
package reporter

import (
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/tinovyatkin/tally/internal/rules"
)

// ReportMetadata contains contextual information about the lint run.
type ReportMetadata struct {
	// FilesScanned is the total number of files that were scanned.
	FilesScanned int
	// RulesEnabled is the total number of rules that were active (not "off").
	RulesEnabled int
}

// Reporter formats and outputs lint violations.
type Reporter interface {
	// Report writes violations to the configured output.
	// The metadata parameter provides context like files scanned and rules enabled.
	Report(violations []rules.Violation, sources map[string][]byte, metadata ReportMetadata) error
}

// SortViolations sorts violations by file, line, column, and rule code for stable output.
// Uses SliceStable and compares all position fields plus rule code to ensure deterministic order.
func SortViolations(violations []rules.Violation) []rules.Violation {
	sorted := make([]rules.Violation, len(violations))
	copy(sorted, violations)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Location.File != sorted[j].Location.File {
			return sorted[i].Location.File < sorted[j].Location.File
		}
		if sorted[i].Location.Start.Line != sorted[j].Location.Start.Line {
			return sorted[i].Location.Start.Line < sorted[j].Location.Start.Line
		}
		if sorted[i].Location.Start.Column != sorted[j].Location.Start.Column {
			return sorted[i].Location.Start.Column < sorted[j].Location.Start.Column
		}
		return sorted[i].RuleCode < sorted[j].RuleCode
	})
	return sorted
}

// Format represents an output format type.
type Format string

const (
	// FormatText is human-readable terminal output.
	FormatText Format = "text"
	// FormatJSON is machine-readable JSON output.
	FormatJSON Format = "json"
	// FormatSARIF is Static Analysis Results Interchange Format.
	FormatSARIF Format = "sarif"
	// FormatGitHubActions is GitHub Actions workflow command output.
	FormatGitHubActions Format = "github-actions"
	// FormatMarkdown is concise markdown tables for AI agents.
	FormatMarkdown Format = "markdown"
)

// ParseFormat parses a format string into a Format type.
// Returns an error if the format is unknown.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "text", "":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "sarif":
		return FormatSARIF, nil
	case "github-actions", "github":
		return FormatGitHubActions, nil
	case "markdown", "md":
		return FormatMarkdown, nil
	default:
		return "", fmt.Errorf("unknown format: %q (valid: text, json, sarif, github-actions, markdown)", s)
	}
}

// Options configures reporter creation.
type Options struct {
	// Format specifies the output format.
	Format Format

	// Writer is the output destination.
	Writer io.Writer

	// Color enables/disables colored output (text format only).
	// nil means auto-detect.
	Color *bool

	// ShowSource enables source code snippets (text format only).
	ShowSource bool

	// ToolVersion is included in SARIF output.
	ToolVersion string

	// ToolName is the tool name for SARIF output.
	ToolName string

	// ToolURI is the tool information URI for SARIF output.
	ToolURI string
}

// DefaultOptions returns sensible defaults for reporter options.
func DefaultOptions() Options {
	return Options{
		Format:      FormatText,
		Writer:      os.Stdout,
		Color:       nil, // auto-detect
		ShowSource:  true,
		ToolName:    "tally",
		ToolURI:     "https://github.com/tinovyatkin/tally",
		ToolVersion: "dev",
	}
}

// New creates a reporter based on the format specified in options.
func New(opts Options) (Reporter, error) {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}

	switch opts.Format {
	case FormatText, "":
		textOpts := TextOptions{
			Color: opts.Color,
			// Enable syntax highlighting when color is auto-detected (nil) or explicitly enabled
			SyntaxHighlight: opts.Color == nil || *opts.Color,
			ShowSource:      opts.ShowSource,
		}
		return &textReporterAdapter{
			reporter: NewTextReporter(textOpts),
			writer:   opts.Writer,
		}, nil

	case FormatJSON:
		return NewJSONReporter(opts.Writer), nil

	case FormatSARIF:
		return NewSARIFReporter(opts.Writer, opts.ToolName, opts.ToolVersion, opts.ToolURI), nil

	case FormatGitHubActions:
		return NewGitHubActionsReporter(opts.Writer), nil

	case FormatMarkdown:
		return NewMarkdownReporter(opts.Writer), nil

	default:
		return nil, fmt.Errorf("unknown format: %q", opts.Format)
	}
}

// textReporterAdapter adapts TextReporter to the Reporter interface.
type textReporterAdapter struct {
	reporter *TextReporter
	writer   io.Writer
}

// Report implements Reporter.
func (a *textReporterAdapter) Report(violations []rules.Violation, sources map[string][]byte, _ ReportMetadata) error {
	return a.reporter.Print(a.writer, violations, sources)
}

// GetWriter returns an io.Writer for the given output path.
// Supports "stdout", "stderr", or file paths.
func GetWriter(path string) (io.Writer, func() error, error) {
	switch path {
	case "stdout", "":
		return os.Stdout, func() error { return nil }, nil
	case "stderr":
		return os.Stderr, func() error { return nil }, nil
	default:
		f, err := os.Create(path)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create output file: %w", err)
		}
		return f, f.Close, nil
	}
}
