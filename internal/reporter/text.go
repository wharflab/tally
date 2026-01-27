// Package reporter provides output formatters for lint results.
//
// The text formatter is adapted from BuildKit's linter output format
// with enhancements using Lip Gloss for styling and Chroma for syntax highlighting.
package reporter

import (
	"bytes"
	"fmt"
	"io"
	"sort"
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
	opts      TextOptions
	lexer     chroma.Lexer
	formatter chroma.Formatter
	style     *chroma.Style
}

// NewTextReporter creates a new text reporter with the given options.
func NewTextReporter(opts TextOptions) *TextReporter {
	r := &TextReporter{opts: opts}

	// Determine if colors should be used
	colorEnabled := useColors
	if opts.Color != nil {
		colorEnabled = *opts.Color
	}

	if colorEnabled && opts.SyntaxHighlight {
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
	// Sort violations by file, then by line
	sorted := make([]rules.Violation, len(violations))
	copy(sorted, violations)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Location.File != sorted[j].Location.File {
			return sorted[i].Location.File < sorted[j].Location.File
		}
		return sorted[i].Location.Start.Line < sorted[j].Location.Start.Line
	})

	for _, v := range sorted {
		if err := r.printViolation(w, v, sources[v.Location.File]); err != nil {
			return err
		}
	}
	return nil
}

// printViolation formats a single violation.
func (r *TextReporter) printViolation(w io.Writer, v rules.Violation, source []byte) error {
	colorEnabled := useColors
	if r.opts.Color != nil {
		colorEnabled = *r.opts.Color
	}

	// Get severity style
	sevStyle, ok := severityStyles[v.Severity]
	if !ok {
		sevStyle = warningStyle
	}

	// Header line: SEVERITY: RuleCode - URL
	var header string
	if colorEnabled {
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
	fmt.Fprintln(w, header)

	// Message
	if colorEnabled {
		fmt.Fprintln(w, messageStyle.Render(v.Message))
	} else {
		fmt.Fprintln(w, v.Message)
	}

	// Source snippet
	if r.opts.ShowSource && !v.Location.IsFileLevel() && len(source) > 0 {
		r.printSource(w, v.Location, source, colorEnabled)
	}

	return nil
}

// printSource renders the source code snippet with optional syntax highlighting.
func (r *TextReporter) printSource(w io.Writer, loc rules.Location, source []byte, colorEnabled bool) {
	lines := strings.Split(string(source), "\n")

	// Get start/end lines (BuildKit uses 1-based line numbers)
	start := loc.Start.Line
	end := loc.End.Line
	if loc.IsPointLocation() || end < start {
		end = start
	}

	// Bounds check
	if start > len(lines) || start < 1 {
		return
	}
	if end > len(lines) {
		end = len(lines)
	}

	// Calculate padding (2-4 lines of context)
	pad := 2
	if end == start {
		pad = 4
	}

	displayStart := start
	p := 0
	for p < pad {
		expanded := false
		if start > 1 {
			start--
			p++
			expanded = true
		}
		if end < len(lines) {
			end++
			p++
			expanded = true
		}
		if !expanded {
			break
		}
	}

	// File:line header
	fmt.Fprintln(w)
	if colorEnabled {
		fmt.Fprintln(w, fileLocStyle.Render(fmt.Sprintf("%s:%d", loc.File, displayStart)))
		fmt.Fprintln(w, separatorStyle.Render("────────────────────"))
	} else {
		fmt.Fprintf(w, "%s:%d\n", loc.File, displayStart)
		fmt.Fprintln(w, "--------------------")
	}

	// Print lines with optional syntax highlighting
	for i := start; i <= end; i++ {
		isAffected := lineInRange(i, loc.Start.Line, loc.End.Line)
		lineContent := strings.TrimSuffix(lines[i-1], "\r") // Trim CRLF to avoid artifacts

		// Format line number
		var lineNum string
		if colorEnabled {
			lineNum = lineNumStyle.Render(fmt.Sprintf(" %3d │", i))
		} else {
			lineNum = fmt.Sprintf(" %3d |", i)
		}

		// Format marker
		var marker string
		if isAffected {
			if colorEnabled {
				marker = markerStyle.Render(">>>")
			} else {
				marker = ">>>"
			}
		} else {
			marker = "   "
		}

		// Format line content with optional syntax highlighting
		var content string
		if colorEnabled && r.lexer != nil && r.style != nil && r.formatter != nil {
			content = r.highlightLine(lineContent)
		} else {
			content = lineContent
		}

		fmt.Fprintf(w, "%s %s %s\n", lineNum, marker, content)
	}

	// Closing separator
	if colorEnabled {
		fmt.Fprintln(w, separatorStyle.Render("────────────────────"))
	} else {
		fmt.Fprintln(w, "--------------------")
	}
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
