package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

// expectedIndent is the indentation string used by this rule: a single tab.
// Tabs are the only supported indent because Docker heredoc syntax (<<-)
// strips leading tabs from body lines. Spaces have no equivalent shell
// whitespace treatment, so using them would corrupt heredoc content.
const expectedIndent = "\t"

// ConsistentIndentationRule implements the consistent-indentation linting rule.
// For multi-stage Dockerfiles, it enforces indentation of commands within each stage.
// For single-stage Dockerfiles, it enforces no indentation (flat style).
type ConsistentIndentationRule struct{}

// NewConsistentIndentationRule creates a new consistent-indentation rule instance.
func NewConsistentIndentationRule() *ConsistentIndentationRule {
	return &ConsistentIndentationRule{}
}

// Metadata returns the rule metadata.
func (r *ConsistentIndentationRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.TallyRulePrefix + "consistent-indentation",
		Name:            "Consistent Indentation",
		Description:     "Enforces consistent indentation for Dockerfile build stages",
		DocURL:          "https://github.com/tinovyatkin/tally/blob/main/docs/rules/tally/consistent-indentation.md",
		DefaultSeverity: rules.SeverityOff,
		Category:        "style",
		IsExperimental:  true,
		FixPriority:     50, // After content fixes (casing at 0) but before structural (heredoc at 100+)
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *ConsistentIndentationRule) Schema() map[string]any {
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"additionalProperties": false,
	}
}

// DefaultConfig returns the default configuration for this rule.
func (r *ConsistentIndentationRule) DefaultConfig() any {
	return nil
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *ConsistentIndentationRule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

// Check runs the consistent-indentation rule.
func (r *ConsistentIndentationRule) Check(input rules.LintInput) []rules.Violation {
	isMultiStage := len(input.Stages) > 1
	sm := input.SourceMap()
	meta := r.Metadata()

	var violations []rules.Violation

	// Global ARGs before first FROM should never be indented
	for _, arg := range input.MetaArgs {
		violations = append(violations,
			r.checkNodeNoIndent(input.File, sm, arg.Location(), meta)...)
	}

	for _, stage := range input.Stages {
		// FROM lines should never be indented
		violations = append(violations,
			r.checkNodeNoIndent(input.File, sm, stage.Location, meta)...)

		for _, cmd := range stage.Commands {
			if isMultiStage {
				// Multi-stage: commands within each stage should be indented
				violations = append(violations,
					r.checkCommandIndented(input.File, sm, cmd.Location(), meta)...)
			} else {
				// Single-stage: no indentation expected
				violations = append(violations,
					r.checkNodeNoIndent(input.File, sm, cmd.Location(), meta)...)
			}
		}
	}

	return violations
}

// checkNodeNoIndent checks that an instruction's lines have no leading whitespace.
// Checks all lines including backslash continuations.
func (r *ConsistentIndentationRule) checkNodeNoIndent(
	file string,
	sm *sourcemap.SourceMap,
	location []parser.Range,
	meta rules.RuleMetadata,
) []rules.Violation {
	if len(location) == 0 {
		return nil
	}

	startLine := location[0].Start.Line
	endLine := resolveEndLine(sm, location)

	// Check all lines — flag if any line has indentation
	var firstBadLine int
	var firstBadIndent string
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		indent := leadingWhitespace(sm.Line(lineNum - 1))
		if indent != "" {
			firstBadLine = lineNum
			firstBadIndent = indent
			break
		}
	}
	if firstBadLine == 0 {
		return nil
	}

	loc := rules.NewRangeLocation(file, firstBadLine, 0, firstBadLine, len(firstBadIndent))
	v := rules.NewViolation(
		loc,
		meta.Code,
		"unexpected indentation; this line should not be indented",
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithSuggestedFix(&rules.SuggestedFix{
		Description: "Remove indentation",
		Safety:      rules.FixSafe,
		Priority:    meta.FixPriority,
		Edits:       r.removeIndentEdits(file, sm, location, endLine),
		IsPreferred: true,
	})

	return []rules.Violation{v}
}

// checkCommandIndented checks that a command's lines are indented with the expected indent (1 tab).
// Checks all lines including backslash continuations.
func (r *ConsistentIndentationRule) checkCommandIndented(
	file string,
	sm *sourcemap.SourceMap,
	location []parser.Range,
	meta rules.RuleMetadata,
) []rules.Violation {
	if len(location) == 0 {
		return nil
	}

	startLine := location[0].Start.Line
	endLine := resolveEndLine(sm, location)

	// Check all lines — flag if any line has wrong indentation
	var firstBadLine int
	var firstBadIndent string
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		indent := leadingWhitespace(sm.Line(lineNum - 1))
		if indent != expectedIndent {
			firstBadLine = lineNum
			firstBadIndent = indent
			break
		}
	}
	if firstBadLine == 0 {
		return nil
	}

	// Determine the issue based on the first misindented line
	var message string
	switch {
	case firstBadIndent == "":
		message = "missing indentation; expected 1 tab"
	case consistsOf(firstBadIndent, "\t"):
		message = "wrong indentation width; expected 1 tab, got " + describeIndent(firstBadIndent)
	default:
		message = "wrong indentation style; expected 1 tab, got " + describeIndent(firstBadIndent)
	}

	loc := rules.NewRangeLocation(file, firstBadLine, 0, firstBadLine, len(firstBadIndent))
	v := rules.NewViolation(
		loc,
		meta.Code,
		message,
		meta.DefaultSeverity,
	).WithDocURL(meta.DocURL).WithSuggestedFix(&rules.SuggestedFix{
		Description: "Fix indentation to 1 tab",
		Safety:      rules.FixSafe,
		Priority:    meta.FixPriority,
		Edits:       r.setIndentEdits(file, sm, location, endLine, expectedIndent),
		IsPreferred: true,
	})

	return []rules.Violation{v}
}

// removeIndentEdits generates TextEdits to remove leading whitespace from all lines of a node.
func (r *ConsistentIndentationRule) removeIndentEdits(
	file string,
	sm *sourcemap.SourceMap,
	location []parser.Range,
	endLine int,
) []rules.TextEdit {
	if len(location) == 0 {
		return nil
	}

	return r.setIndentEdits(file, sm, location, endLine, "")
}

// resolveEndLine returns the last line number of an instruction, including
// backslash continuation lines. BuildKit's parser may report End.Line ==
// Start.Line for multi-line instructions joined by \, so we scan the source
// for continuations.
func resolveEndLine(sm *sourcemap.SourceMap, location []parser.Range) int {
	endLine := location[0].End.Line
	endLine = min(endLine, sm.LineCount())
	for l := endLine; l <= sm.LineCount(); l++ {
		line := sm.Line(l - 1) // l is 1-based, sm.Line is 0-based
		if !strings.HasSuffix(strings.TrimRight(line, " \t"), `\`) {
			return l
		}
		endLine = min(l+1, sm.LineCount()) // clamp to last line
	}
	return endLine
}

// setIndentEdits generates TextEdits to set indentation on all lines of a node.
// endLine is the precomputed last line number (from resolveEndLine).
func (r *ConsistentIndentationRule) setIndentEdits(
	file string,
	sm *sourcemap.SourceMap,
	location []parser.Range,
	endLine int,
	indent string,
) []rules.TextEdit {
	if len(location) == 0 {
		return nil
	}

	startLine := location[0].Start.Line

	var edits []rules.TextEdit
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		line := sm.Line(lineNum - 1) // 0-based
		currentIndent := leadingWhitespace(line)

		if currentIndent != indent {
			// Replace current indentation with expected
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(file, lineNum, 0, lineNum, len(currentIndent)),
				NewText:  indent,
			})
		}
	}

	// Convert << to <<- on all lines so that BuildKit strips
	// leading tabs from heredoc body lines. Heredocs may appear
	// on any line, including backslash continuations.
	if indent != "" {
		for lineNum := startLine; lineNum <= endLine; lineNum++ {
			ln := sm.Line(lineNum - 1)
			if edit := heredocDashEdit(file, lineNum, ln); edit != nil {
				edits = append(edits, *edit)
			}
		}
	}

	return edits
}

// heredocDashEdit returns a TextEdit to convert << to <<- on a line that
// contains a heredoc operator, or nil if no conversion is needed.
// This is necessary when tab indentation is applied to heredoc instructions,
// because <<- tells BuildKit to strip leading tabs from the body.
func heredocDashEdit(file string, lineNum int, line string) *rules.TextEdit {
	idx := strings.Index(line, "<<")
	if idx < 0 {
		return nil
	}
	// Heredoc operator must be preceded by whitespace or start-of-line,
	// otherwise it could be inside a quoted string (e.g., RUN echo "<<EOF").
	if idx > 0 && line[idx-1] != ' ' && line[idx-1] != '\t' {
		return nil
	}
	// Already <<-
	if idx+2 < len(line) && line[idx+2] == '-' {
		return nil
	}
	// Must be followed by an alpha character (delimiter name like EOF, CONTENT)
	if idx+2 >= len(line) {
		return nil
	}
	ch := line[idx+2]
	if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') {
		return nil
	}
	// Insert "-" between << and the delimiter
	return &rules.TextEdit{
		Location: rules.NewRangeLocation(file, lineNum, idx+2, lineNum, idx+2),
		NewText:  "-",
	}
}

// leadingWhitespace returns the leading whitespace of a line.
func leadingWhitespace(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(trimmed)]
}

// consistsOf checks whether every character in s is found in chars.
func consistsOf(s, chars string) bool {
	if chars == "" || s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune(chars, c) {
			return false
		}
	}
	return true
}

// describeIndent returns a human-readable description of an indentation string.
func describeIndent(indent string) string {
	if indent == "" {
		return "no indentation"
	}

	tabs := strings.Count(indent, "\t")
	spaces := strings.Count(indent, " ")

	switch {
	case tabs > 0 && spaces == 0:
		if tabs == 1 {
			return "1 tab"
		}
		return fmt.Sprintf("%d tabs", tabs)
	case spaces > 0 && tabs == 0:
		if spaces == 1 {
			return "1 space"
		}
		return fmt.Sprintf("%d spaces", spaces)
	default:
		return fmt.Sprintf("%d mixed characters", len(indent))
	}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewConsistentIndentationRule())
}
