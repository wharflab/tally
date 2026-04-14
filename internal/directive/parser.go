package directive

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/sourcemap"
)

// Regex patterns for directive parsing.
// All patterns are case-insensitive for the directive keywords.
// Patterns capture an optional reason after `;reason=` (BuildKit-style separator).
// Rule lists allow optional whitespace around commas (e.g., "DL3006, DL3008").
// Rule names can include / for namespaced rules (e.g., "buildkit/StageNameCasing").
var (
	// # tally [global] ignore=RULE1,RULE2[;reason=explanation]
	tallyPattern = regexp.MustCompile(
		`(?i)#\s*tally\s+(global\s+)?ignore\s*=\s*([A-Za-z0-9_,\s/.-]+?)(?:;reason\s*=\s*(.*))?$`)

	// # hadolint [global] ignore=RULE1,RULE2[;reason=explanation]
	// Note: ;reason= is a tally extension, not part of hadolint's native syntax
	hadolintPattern = regexp.MustCompile(
		`(?i)#\s*hadolint\s+(global\s+)?ignore\s*=\s*([A-Za-z0-9_,\s/.-]+?)(?:;reason\s*=\s*(.*))?$`)

	// # check=skip=RULE1,RULE2[;reason=explanation] (buildx - always file-level/global)
	// Note: ;reason= is a tally extension, BuildKit silently ignores it
	buildxPattern = regexp.MustCompile(
		`(?i)#\s*check\s*=\s*skip\s*=\s*([A-Za-z0-9_,\s/.-]+?)(?:;reason\s*=\s*(.*))?$`)

	// # tally shell=<shell>
	// Shell names can include dots for extensions (e.g., cmd.exe)
	tallyShellPattern = regexp.MustCompile(
		`(?i)#\s*tally\s+shell\s*=\s*([A-Za-z0-9_./-]+)\s*$`)

	// # hadolint shell=<shell>
	// Shell names can include dots for extensions (e.g., cmd.exe)
	hadolintShellPattern = regexp.MustCompile(
		`(?i)#\s*hadolint\s+shell\s*=\s*([A-Za-z0-9_./-]+)\s*$`)
)

// RuleValidator is a function that checks if a rule code is known.
// Returns true if the rule exists in the registry.
type RuleValidator func(string) bool

// InstructionSpan represents the (inclusive) span of a single Dockerfile instruction.
// Line numbers are 0-based to match SourceMap conventions.
type InstructionSpan struct {
	StartLine int
	EndLine   int
}

// InstructionSpanIndex enables efficient lookup of the next instruction span
// after a given line.
type InstructionSpanIndex struct {
	Spans []InstructionSpan // Sorted by StartLine ascending.
}

// NewInstructionSpanIndexFromAST builds an index from a BuildKit parser result.
func NewInstructionSpanIndexFromAST(ast *parser.Result, sm *sourcemap.SourceMap) *InstructionSpanIndex {
	if ast == nil || ast.AST == nil || sm == nil {
		return nil
	}

	spans := make([]InstructionSpan, 0, len(ast.AST.Children))
	for _, node := range ast.AST.Children {
		if node == nil {
			continue
		}
		start := node.StartLine - 1 // 0-based
		end := sm.ResolveEndLineWithEscape(node.EndLine, ast.EscapeToken) - 1
		if start < 0 || end < start {
			continue
		}
		spans = append(spans, InstructionSpan{StartLine: start, EndLine: end})
	}

	// Children are already in source order; StartLine is monotonic.
	return &InstructionSpanIndex{Spans: spans}
}

// NewInstructionSpanIndexFromSource parses the Dockerfile source and builds a span index.
// Returns nil if parsing fails.
func NewInstructionSpanIndexFromSource(source []byte, sm *sourcemap.SourceMap) *InstructionSpanIndex {
	if sm == nil {
		return nil
	}
	res, err := parser.Parse(bytes.NewReader(source))
	if err != nil {
		return nil
	}
	return NewInstructionSpanIndexFromAST(res, sm)
}

// ContainingSpan returns the instruction span that contains the given 0-based line,
// or false if no span contains it.
func (idx *InstructionSpanIndex) ContainingSpan(line int) (InstructionSpan, bool) {
	if idx == nil {
		return InstructionSpan{}, false
	}
	for _, span := range idx.Spans {
		if line >= span.StartLine && line <= span.EndLine {
			return span, true
		}
		if span.StartLine > line {
			break // spans are sorted; no later span can contain this line
		}
	}
	return InstructionSpan{}, false
}

func (idx *InstructionSpanIndex) nextInstructionSpan(afterLine int) (InstructionSpan, bool) {
	if idx == nil || len(idx.Spans) == 0 {
		return InstructionSpan{}, false
	}
	// Linear scan is fine: Dockerfiles are small. Keep this simple and robust.
	for _, span := range idx.Spans {
		if span.StartLine > afterLine {
			return span, true
		}
	}
	return InstructionSpan{}, false
}

// Parse extracts all inline directives from a SourceMap.
// If validator is non-nil, unknown rule codes generate parse errors.
func Parse(sm *sourcemap.SourceMap, validator RuleValidator, spanIndex *InstructionSpanIndex) *ParseResult {
	result := &ParseResult{}
	comments := sm.Comments()

	for _, comment := range comments {
		if !comment.IsDirective {
			continue
		}

		// Try ignore directive patterns first
		if d, err := parseTally(comment, sm, spanIndex); d != nil || err != nil {
			if err != nil {
				result.Errors = append(result.Errors, *err)
			}
			if d != nil {
				validateDirective(d, validator, result)
			}
			continue
		}

		if d, err := parseHadolint(comment, sm, spanIndex); d != nil || err != nil {
			if err != nil {
				result.Errors = append(result.Errors, *err)
			}
			if d != nil {
				validateDirective(d, validator, result)
			}
			continue
		}

		if d, err := parseBuildx(comment); d != nil || err != nil {
			if err != nil {
				result.Errors = append(result.Errors, *err)
			}
			if d != nil {
				validateDirective(d, validator, result)
			}
			continue
		}

		// Try shell directive patterns
		if sd := parseTallyShell(comment); sd != nil {
			result.ShellDirectives = append(result.ShellDirectives, *sd)
			continue
		}

		if sd := parseHadolintShell(comment); sd != nil {
			result.ShellDirectives = append(result.ShellDirectives, *sd)
			continue
		}
	}

	return result
}

// validateDirective validates rule codes and adds the directive or errors.
func validateDirective(d *Directive, validator RuleValidator, result *ParseResult) {
	if validator != nil {
		unknownRules := []string{}
		for _, rule := range d.Rules {
			if rule != "all" && !validator(rule) {
				unknownRules = append(unknownRules, rule)
			}
		}
		if len(unknownRules) > 0 {
			result.Errors = append(result.Errors, ParseError{
				Line:    d.Line,
				Message: "unknown rule code(s): " + strings.Join(unknownRules, ", "),
				RawText: d.RawText,
			})
		}
	}
	result.Directives = append(result.Directives, *d)
}

// parseIgnoreDirective parses a directive with pattern matching [global] ignore=RULES format.
// Used by both tally and hadolint parsers to avoid code duplication.
func parseIgnoreDirective(
	comment sourcemap.Comment,
	sm *sourcemap.SourceMap,
	pattern *regexp.Regexp,
	source DirectiveSource,
	spanIndex *InstructionSpanIndex,
) (*Directive, *ParseError) {
	matches := pattern.FindStringSubmatch(comment.Text)
	if matches == nil {
		return nil, nil
	}

	isGlobal := strings.TrimSpace(matches[1]) != ""
	rulesStr := matches[2]

	// Extract optional reason from capture group 3
	var reason string
	if len(matches) > 3 {
		reason = strings.TrimSpace(matches[3])
	}

	rules, err := parseRuleList(rulesStr)
	if err != nil {
		return nil, &ParseError{
			Line:    comment.Line,
			Message: err.Error(),
			RawText: comment.Text,
		}
	}

	d := &Directive{
		Rules:   rules,
		Line:    comment.Line,
		RawText: comment.Text,
		Source:  source,
		Reason:  reason,
	}

	if isGlobal {
		d.Type = TypeGlobal
		d.AppliesTo = GlobalRange()
	} else {
		d.Type = TypeNextLine
		d.AppliesTo = nextInstructionLineRange(comment.Line, sm, spanIndex)
	}

	return d, nil
}

// parseTally attempts to parse a tally-format directive.
func parseTally(comment sourcemap.Comment, sm *sourcemap.SourceMap, spanIndex *InstructionSpanIndex) (*Directive, *ParseError) {
	return parseIgnoreDirective(comment, sm, tallyPattern, SourceTally, spanIndex)
}

// parseHadolint attempts to parse a hadolint-format directive.
func parseHadolint(comment sourcemap.Comment, sm *sourcemap.SourceMap, spanIndex *InstructionSpanIndex) (*Directive, *ParseError) {
	return parseIgnoreDirective(comment, sm, hadolintPattern, SourceHadolint, spanIndex)
}

// parseBuildx attempts to parse a buildx-format directive.
// buildx's check=skip is always file-level (global).
// Note: ;reason= is a tally extension; BuildKit silently ignores unknown options.
func parseBuildx(comment sourcemap.Comment) (*Directive, *ParseError) {
	matches := buildxPattern.FindStringSubmatch(comment.Text)
	if matches == nil {
		return nil, nil
	}

	rulesStr := matches[1]

	// Extract optional reason from capture group 2
	var reason string
	if len(matches) > 2 {
		reason = strings.TrimSpace(matches[2])
	}

	rules, err := parseRuleList(rulesStr)
	if err != nil {
		return nil, &ParseError{
			Line:    comment.Line,
			Message: err.Error(),
			RawText: comment.Text,
		}
	}

	return &Directive{
		Type:      TypeGlobal, // buildx check=skip is always global
		Rules:     rules,
		Line:      comment.Line,
		AppliesTo: GlobalRange(),
		RawText:   comment.Text,
		Source:    SourceBuildx,
		Reason:    reason,
	}, nil
}

// parseRuleList parses a comma-separated list of rule codes.
// Returns an error if the list is empty.
func parseRuleList(s string) ([]string, error) {
	if s == "" {
		return nil, &parseRuleError{msg: "empty rule list"}
	}

	parts := strings.Split(s, ",")
	rules := make([]string, 0, len(parts))

	for _, part := range parts {
		rule := strings.TrimSpace(part)
		if rule == "" {
			continue
		}
		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		return nil, &parseRuleError{msg: "empty rule list"}
	}

	return rules, nil
}

type parseRuleError struct {
	msg string
}

func (e *parseRuleError) Error() string {
	return e.msg
}

// nextNonCommentLineRange finds the range for the next non-comment line.
// If there is no next line (directive at end of file), returns an empty range
// that won't match any line.
func nextNonCommentLineRange(directiveLine int, sm *sourcemap.SourceMap) LineRange {
	lineCount := sm.LineCount()

	for i := directiveLine + 1; i < lineCount; i++ {
		line := strings.TrimSpace(sm.Line(i))
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Found the next non-comment line
		return LineRange{Start: i, End: i}
	}

	// No non-comment line found after the directive
	// Return a range that won't match anything
	return LineRange{Start: -1, End: -1}
}

// parseShellDirective parses a shell directive with the given pattern and source.
func parseShellDirective(comment sourcemap.Comment, pattern *regexp.Regexp, source DirectiveSource) *ShellDirective {
	matches := pattern.FindStringSubmatch(comment.Text)
	if matches == nil {
		return nil
	}

	return &ShellDirective{
		Shell:   strings.ToLower(matches[1]),
		Line:    comment.Line,
		Source:  source,
		RawText: comment.Text,
	}
}

// parseTallyShell attempts to parse a tally shell directive.
func parseTallyShell(comment sourcemap.Comment) *ShellDirective {
	return parseShellDirective(comment, tallyShellPattern, SourceTally)
}

// parseHadolintShell attempts to parse a hadolint shell directive.
func parseHadolintShell(comment sourcemap.Comment) *ShellDirective {
	return parseShellDirective(comment, hadolintShellPattern, SourceHadolint)
}
