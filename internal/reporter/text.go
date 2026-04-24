// Package reporter provides output formatters for lint results.
package reporter

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"

	"github.com/wharflab/tally/internal/highlight"
	highlightcore "github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/highlight/renderansi"
	"github.com/wharflab/tally/internal/highlight/theme"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// Styles for different parts of the output
var (
	// Color detection using termenv (respects NO_COLOR, CLICOLOR_FORCE, terminal detection)
	useColors = termenv.EnvColorProfile() != termenv.Ascii

	// Warning header style
	warningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214")) // Orange/Yellow

	// Rule code style
	ruleCodeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")) // Red

	// URL style
	urlStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")). // Blue
			Underline(true)

	// Message style
	messageStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")) // White

	// File location style
	fileLocStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("252")) // Light gray

	// Line number style
	lineNumStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")) // Dark gray

	// Separator style
	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")) // Darker gray

	// Marker style for affected lines
	markerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")) // Red

	// Placeholder style for empty or whitespace-only source lines.
	blankLineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)

	// Severity styles
	severityStyles = map[rules.Severity]lipgloss.Style{
		rules.SeverityError: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")), // Red
		rules.SeverityWarning: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("214")), // Orange
		rules.SeverityInfo: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")), // Blue
		rules.SeverityStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("245")), // Gray
	}
)

// TextOptions configures the text reporter output.
type TextOptions struct {
	// Color enables/disables colored output. Default: auto-detect.
	Color *bool

	// SyntaxHighlight enables semantic snippet highlighting.
	SyntaxHighlight bool

	// ShowSource shows source code snippets. Default: true.
	ShowSource bool

	// Theme controls color palette selection for snippets: auto, dark, or light.
	Theme string
}

// DefaultTextOptions returns sensible defaults for text output.
func DefaultTextOptions() TextOptions {
	return TextOptions{
		Color:           nil, // auto-detect
		SyntaxHighlight: true,
		ShowSource:      true,
		Theme:           string(theme.ModeAuto),
	}
}

// TextReporter formats violations as styled text output.
type TextReporter struct {
	opts         TextOptions
	colorEnabled bool
	palette      theme.Palette
	docCache     map[string]*highlight.Document
}

// NewTextReporter creates a new text reporter with the given options.
func NewTextReporter(opts TextOptions) *TextReporter {
	r := &TextReporter{
		opts:     opts,
		docCache: make(map[string]*highlight.Document),
	}

	// Determine if colors should be used
	r.colorEnabled = useColors
	if opts.Color != nil {
		r.colorEnabled = *opts.Color
	}
	r.palette = theme.Resolve(r.colorEnabled && opts.SyntaxHighlight, opts.Theme)

	return r
}

// Print writes violations to the writer.
func (r *TextReporter) Print(w io.Writer, violations []rules.Violation, sources map[string][]byte) error {
	return r.PrintReport(w, violations, sources, ReportMetadata{})
}

// PrintReport writes violations and optional run metadata to the writer.
func (r *TextReporter) PrintReport(w io.Writer, violations []rules.Violation, sources map[string][]byte, metadata ReportMetadata) error {
	r.docCache = make(map[string]*highlight.Document, len(sources))
	sorted := SortViolations(violations)

	lastLabel := ""
	for _, v := range sorted {
		label := InvocationLabel(v)
		if label != "" && label != lastLabel {
			if _, err := fmt.Fprintf(w, "\n[%s]\n", label); err != nil {
				return err
			}
			lastLabel = label
		}
		if err := r.printViolation(w, v, sources[v.Location.File]); err != nil {
			return err
		}
	}
	if metadata.InvocationsScanned > 0 {
		if _, err := fmt.Fprintf(w, "\nSummary: %d Dockerfiles, %d invocations, %d violations.\n",
			metadata.FilesScanned, metadata.InvocationsScanned, len(violations)); err != nil {
			return err
		}
	}
	return nil
}

// printViolation formats a single violation.
func (r *TextReporter) printViolation(w io.Writer, v rules.Violation, source []byte) error {
	// Get severity style
	sevStyle, ok := severityStyles[v.Severity]
	if !ok {
		sevStyle = warningStyle
	}

	// Header line: SEVERITY: RuleCode - URL
	var header string
	if r.colorEnabled {
		sevLabel := strings.ToUpper(v.Severity.String())
		header = fmt.Sprintf("\n%s %s",
			sevStyle.Render(sevLabel+":"),
			ruleCodeStyle.Render(v.RuleCode))
		if v.DocURL != "" {
			header += " - " + urlStyle.Render(v.DocURL)
		}
	} else {
		header = fmt.Sprintf("\n%s: %s", strings.ToUpper(v.Severity.String()), v.RuleCode)
		if v.DocURL != "" {
			header += " - " + v.DocURL
		}
	}
	if _, err := fmt.Fprintln(w, header); err != nil {
		return err
	}

	// Message
	if r.colorEnabled {
		if _, err := fmt.Fprintln(w, messageStyle.Render(v.Message)); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(w, v.Message); err != nil {
			return err
		}
	}

	// Source snippet
	if r.opts.ShowSource && !v.Location.IsFileLevel() && len(source) > 0 {
		if err := r.printSource(w, v.Location, source); err != nil {
			return err
		}
	}

	return nil
}

// printSource renders the source code snippet with optional syntax highlighting.
func (r *TextReporter) printSource(w io.Writer, loc rules.Location, source []byte) error {
	doc := r.highlightDocument(loc.File, source)
	lines := doc.SourceMap.Lines()

	// Get start/end lines (BuildKit uses 1-based line numbers)
	start := loc.Start.Line
	end := loc.End.Line
	if loc.IsPointLocation() || end < start {
		end = start
	}

	// Honor exclusive end: when End.Column == 0, the range ends at previous line
	// This matches SnippetForLocation semantics
	markerEnd := end
	if loc.End.Column == 0 && markerEnd > loc.Start.Line {
		markerEnd--
	}

	// Bounds check
	if start > len(lines) || start < 1 {
		return nil
	}
	if end > len(lines) {
		end = len(lines)
	}

	// Expand context padding around the violation
	displayStart := start
	start, end = r.expandContextPadding(start, end, len(lines))

	// Write file:line header
	if err := r.writeSourceHeader(w, loc.File, displayStart); err != nil {
		return err
	}

	// Print lines with optional syntax highlighting
	for i := start; i <= end; i++ {
		if err := r.writeSourceLine(
			w,
			i,
			lines[i-1],
			doc.LineTokens(i-1),
			r.overlayForLine(loc, i, lines[i-1]),
			lineInRange(i, loc.Start.Line, markerEnd),
		); err != nil {
			return err
		}
	}

	// Write closing separator
	return r.writeSeparator(w)
}

// expandContextPadding adds 2-4 lines of context around the violation range.
// Returns (newStart, newEnd) for the expanded range.
func (r *TextReporter) expandContextPadding(start, end, totalLines int) (int, int) {
	pad := 2
	if end == start {
		pad = 4
	}

	p := 0
	for p < pad {
		expanded := false
		if start > 1 {
			start--
			p++
			expanded = true
		}
		if end < totalLines {
			end++
			p++
			expanded = true
		}
		if !expanded {
			break
		}
	}
	return start, end
}

// writeSourceHeader writes the file:line header and opening separator.
func (r *TextReporter) writeSourceHeader(w io.Writer, file string, line int) error {
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if r.colorEnabled {
		if _, err := fmt.Fprintln(w, fileLocStyle.Render(fmt.Sprintf("%s:%d", file, line))); err != nil {
			return err
		}
		return r.writeSeparator(w)
	}
	if _, err := fmt.Fprintf(w, "%s:%d\n", file, line); err != nil {
		return err
	}
	return r.writeSeparator(w)
}

// writeSeparator writes a horizontal separator line.
func (r *TextReporter) writeSeparator(w io.Writer) error {
	if r.colorEnabled {
		_, err := fmt.Fprintln(w, separatorStyle.Render("────────────────────"))
		return err
	}
	_, err := fmt.Fprintln(w, "--------------------")
	return err
}

// writeSourceLine writes a single source line with line number and marker.
func (r *TextReporter) writeSourceLine(
	w io.Writer,
	lineNum int,
	lineContent string,
	lineTokens []highlightcore.Token,
	overlay *renderansi.Overlay,
	isAffected bool,
) error {
	lineContent = strings.TrimSuffix(lineContent, "\r") // Trim CRLF to avoid artifacts

	// Format components
	numStr := r.formatLineNumber(lineNum)
	marker := r.formatMarker(isAffected)
	content := r.formatLineContent(lineContent, lineTokens, overlay)

	_, err := fmt.Fprintf(w, "%s %s %s\n", numStr, marker, content)
	return err
}

// formatLineNumber formats a line number for display.
func (r *TextReporter) formatLineNumber(num int) string {
	if r.colorEnabled {
		return lineNumStyle.Render(fmt.Sprintf(" %3d │", num))
	}
	return fmt.Sprintf(" %3d |", num)
}

// formatMarker formats the affected line marker.
func (r *TextReporter) formatMarker(isAffected bool) string {
	if !isAffected {
		return "   "
	}
	if r.colorEnabled {
		return markerStyle.Render(">>>")
	}
	return ">>>"
}

// formatLineContent formats line content with optional syntax highlighting.
func (r *TextReporter) formatLineContent(
	content string,
	lineTokens []highlightcore.Token,
	overlay *renderansi.Overlay,
) string {
	placeholder, ok := r.placeholderForLine(content)
	if ok {
		if r.colorEnabled {
			return blankLineStyle.Render(placeholder)
		}
		return placeholder
	}
	if r.colorEnabled && r.opts.SyntaxHighlight {
		return renderansi.RenderLine(content, lineTokens, r.palette, overlay)
	}
	return content
}

// PrintText is a convenience function that uses default options.
// This maintains backward compatibility with the original API.
func PrintText(w io.Writer, violations []rules.Violation, sources map[string][]byte) error {
	r := NewTextReporter(DefaultTextOptions())
	return r.Print(w, violations, sources)
}

// PrintTextPlain writes violations without any styling (for non-TTY output).
func PrintTextPlain(w io.Writer, violations []rules.Violation, sources map[string][]byte) error {
	noColor := false
	opts := TextOptions{
		Color:           &noColor,
		SyntaxHighlight: false,
		ShowSource:      true,
	}
	r := NewTextReporter(opts)
	return r.Print(w, violations, sources)
}

// lineInRange checks if a 1-based line number is within the range [start, end].
func lineInRange(line, start, end int) bool {
	if end < start {
		end = start
	}
	return line >= start && line <= end
}

func (r *TextReporter) highlightDocument(file string, source []byte) *highlight.Document {
	if doc, ok := r.docCache[file]; ok {
		return doc
	}
	doc := &highlight.Document{
		File:      file,
		SourceMap: sourcemap.New(source),
	}
	if r.colorEnabled && r.opts.SyntaxHighlight {
		doc = highlight.Analyze(file, source)
	}
	r.docCache[file] = doc
	return doc
}

func (r *TextReporter) overlayForLine(loc rules.Location, lineNum int, line string) *renderansi.Overlay {
	if !r.colorEnabled || !r.opts.SyntaxHighlight {
		return nil
	}
	if loc.IsFileLevel() || loc.End.Line < 0 {
		return nil
	}

	startCol := 0
	endCol := len([]rune(line))
	switch {
	case loc.IsPointLocation():
		if lineNum != loc.Start.Line {
			return nil
		}
		startCol = max(loc.Start.Column, 0)
		endCol = startCol + 1
	case lineNum == loc.Start.Line:
		startCol = max(loc.Start.Column, 0)
		if lineNum == loc.End.Line {
			endCol = loc.End.Column
		}
	case lineNum == loc.End.Line:
		endCol = loc.End.Column
	default:
	}

	lineLen := len([]rune(line))
	if startCol < 0 {
		startCol = 0
	}
	if startCol > lineLen {
		startCol = lineLen
	}
	if endCol > lineLen {
		endCol = lineLen
	}
	if endCol <= startCol {
		return nil
	}
	return &renderansi.Overlay{StartCol: startCol, EndCol: endCol}
}

func (r *TextReporter) placeholderForLine(content string) (string, bool) {
	switch {
	case content == "":
		return "<blank>", true
	case strings.Trim(content, " \t") == "":
		return "<whitespace>", true
	default:
		return "", false
	}
}
