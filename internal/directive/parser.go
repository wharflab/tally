package directive

import (
	"regexp"
	"strings"

	"github.com/tinovyatkin/tally/internal/sourcemap"
)

// Regex patterns for directive parsing.
// All patterns are case-insensitive for the directive keywords.
var (
	// # tally [global] ignore=RULE1,RULE2
	tallyPattern = regexp.MustCompile(`(?i)#\s*tally\s+(global\s+)?ignore\s*=\s*([A-Za-z0-9_,-]+)`)

	// # hadolint [global] ignore=RULE1,RULE2
	hadolintPattern = regexp.MustCompile(`(?i)#\s*hadolint\s+(global\s+)?ignore\s*=\s*([A-Za-z0-9_,-]+)`)

	// # check=skip=RULE1,RULE2 (buildx - always file-level/global)
	buildxPattern = regexp.MustCompile(`(?i)#\s*check\s*=\s*skip\s*=\s*([A-Za-z0-9_,-]+)`)
)

// RuleValidator is a function that checks if a rule code is known.
// Returns true if the rule exists in the registry.
type RuleValidator func(string) bool

// Parse extracts all inline directives from a SourceMap.
// If validator is non-nil, unknown rule codes generate parse errors.
func Parse(sm *sourcemap.SourceMap, validator RuleValidator) *ParseResult {
	result := &ParseResult{}
	comments := sm.Comments()

	for _, comment := range comments {
		if !comment.IsDirective {
			continue
		}

		// Try each pattern in order
		if d, err := parseTally(comment, sm); d != nil || err != nil {
			if err != nil {
				result.Errors = append(result.Errors, *err)
			}
			if d != nil {
				validateDirective(d, validator, result)
			}
			continue
		}

		if d, err := parseHadolint(comment, sm); d != nil || err != nil {
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
) (*Directive, *ParseError) {
	matches := pattern.FindStringSubmatch(comment.Text)
	if matches == nil {
		return nil, nil
	}

	isGlobal := strings.TrimSpace(matches[1]) != ""
	rulesStr := matches[2]

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
	}

	if isGlobal {
		d.Type = TypeGlobal
		d.AppliesTo = GlobalRange()
	} else {
		d.Type = TypeNextLine
		d.AppliesTo = nextNonCommentLineRange(comment.Line, sm)
	}

	return d, nil
}

// parseTally attempts to parse a tally-format directive.
func parseTally(comment sourcemap.Comment, sm *sourcemap.SourceMap) (*Directive, *ParseError) {
	return parseIgnoreDirective(comment, sm, tallyPattern, SourceTally)
}

// parseHadolint attempts to parse a hadolint-format directive.
func parseHadolint(comment sourcemap.Comment, sm *sourcemap.SourceMap) (*Directive, *ParseError) {
	return parseIgnoreDirective(comment, sm, hadolintPattern, SourceHadolint)
}

// parseBuildx attempts to parse a buildx-format directive.
// buildx's check=skip is always file-level (global).
func parseBuildx(comment sourcemap.Comment) (*Directive, *ParseError) {
	matches := buildxPattern.FindStringSubmatch(comment.Text)
	if matches == nil {
		return nil, nil
	}

	rulesStr := matches[1]

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
