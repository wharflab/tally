// Package reporter provides output formatters for lint results.
//
// The text formatter is adapted from BuildKit's linter output format
// with enhancements using Lip Gloss for styling and Chroma for syntax highlighting.
package reporter

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/tinovyatkin/tally/internal/rules"
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

	// SyntaxHighlight enables Dockerfile syntax highlighting in snippets.
	SyntaxHighlight bool

	// ShowSource shows source code snippets. Default: true.
	ShowSource bool

	// ChromaStyle is the Chroma style name for syntax highlighting.
	// Default: "monokai" for dark terminals, "github" for light.
	ChromaStyle string
}

// DefaultTextOptions returns sensible defaults for text output.
func DefaultTextOptions() TextOptions {
	return TextOptions{
		Color:           nil, // auto-detect
		SyntaxHighlight: true,
		ShowSource:      true,
		ChromaStyle:     "", // auto-detect
	}
}

// TextReporter formats violations as styled text output.
type TextReporter struct {
	opts         TextOptions
	colorEnabled bool
	lexer        chroma.Lexer
	formatter    chroma.Formatter
	style        *chroma.Style
}

// NewTextReporter creates a new text reporter with the given options.
func NewTextReporter(opts TextOptions) *TextReporter {
	r := &TextReporter{opts: opts}

	// Determine if colors should be used
	r.colorEnabled = useColors
	if opts.Color != nil {
		r.colorEnabled = *opts.Color
	}

	if r.colorEnabled && opts.SyntaxHighlight {
		r.lexer = lexers.Get("docker")
		if r.lexer == nil {
			r.lexer = lexers.Fallback
		}
		r.lexer = chroma.Coalesce(r.lexer)

		// Select style based on terminal background or user preference
		styleName := opts.ChromaStyle
		if styleName == "" {
			if lipgloss.HasDarkBackground() {
				styleName = "monokai"
			} else {
				styleName = "github"
			}
		}
		r.style = styles.Get(styleName)
		if r.style == nil {
			r.style = styles.Fallback
		}

		r.formatter = formatters.Get("terminal256")
		if r.formatter == nil {
			r.formatter = formatters.Fallback
		}
	}

	return r
}

// Print writes violations to the writer.
func (r *TextReporter) Print(w io.Writer, violations []rules.Violation, sources map[string][]byte) error {
	sorted := SortViolations(violations)

	for _, v := range sorted {
		if err := r.printViolation(w, v, sources[v.Location.File]); err != nil {
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
	lines := strings.Split(string(source), "\n")

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
		if err := r.writeSourceLine(w, i, lines[i-1], lineInRange(i, loc.Start.Line, markerEnd)); err != nil {
			return err
		}
	}

	// Write closing separator
	return r.writeSeparator(w)
}

// expandContextPadding adds 2-4 lines of context around the violation range.
// Returns (newStart, newEnd) for the expanded range.
//
//nolint:gocritic // unnamed results preferred by nonamedreturns linter
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
func (r *TextReporter) writeSourceLine(w io.Writer, lineNum int, lineContent string, isAffected bool) error {
	lineContent = strings.TrimSuffix(lineContent, "\r") // Trim CRLF to avoid artifacts

	// Format components
	numStr := r.formatLineNumber(lineNum)
	marker := r.formatMarker(isAffected)
	content := r.formatLineContent(lineContent)

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
func (r *TextReporter) formatLineContent(content string) string {
	if r.colorEnabled && r.lexer != nil && r.style != nil && r.formatter != nil {
		return r.highlightLine(content)
	}
	return content
}

// highlightLine applies syntax highlighting to a single line.
func (r *TextReporter) highlightLine(line string) string {
	iterator, err := r.lexer.Tokenise(nil, line)
	if err != nil {
		return line
	}

	var buf bytes.Buffer
	err = r.formatter.Format(&buf, r.style, iterator)
	if err != nil {
		return line
	}

	// Trim trailing newline that formatter might add
	return strings.TrimSuffix(buf.String(), "\n")
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
