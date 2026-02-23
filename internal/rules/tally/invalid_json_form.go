package tally

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"strings"
	"unicode"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
)

// InvalidJSONFormRuleCode is the full rule code.
const InvalidJSONFormRuleCode = rules.TallyRulePrefix + "invalid-json-form"

// jsonFormInstructions are instructions that accept JSON exec-form via
// parseMaybeJSON or parseMaybeJSONToList in BuildKit's parser.
var jsonFormInstructions = map[string]bool{
	"cmd":        true,
	"entrypoint": true,
	"run":        true,
	"shell":      true,
	"add":        true,
	"copy":       true,
	"volume":     true,
}

// InvalidJSONFormRule detects instructions that appear to use JSON exec-form
// (arguments start with `[`) but contain invalid JSON.
//
// BuildKit's parser silently treats invalid JSON as shell-form, which almost
// always produces unexpected behavior. For example:
//
//	CMD [bash, -lc, "echo hi"]
//
// is treated as the shell command `[bash, -lc, "echo hi"]` rather than
// exec-form `["bash", "-lc", "echo hi"]`.
//
// Cross-rule interaction with buildkit/JSONArgsRecommended: BuildKit falls
// back to shell-form for invalid JSON, so JSONArgsRecommended (info) also
// fires on the same instruction. The Supersession processor suppresses the
// lower-severity JSONArgsRecommended violation when this rule (error) is
// present at the same line. A cross-rule integration test documents this.
type InvalidJSONFormRule struct{}

// NewInvalidJSONFormRule creates a new invalid-json-form rule instance.
func NewInvalidJSONFormRule() *InvalidJSONFormRule {
	return &InvalidJSONFormRule{}
}

// Metadata returns the rule metadata.
func (r *InvalidJSONFormRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            InvalidJSONFormRuleCode,
		Name:            "Invalid JSON Form",
		Description:     "Arguments appear to use JSON exec-form but contain invalid JSON",
		DocURL:          rules.TallyDocURL(InvalidJSONFormRuleCode),
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Check runs the invalid-json-form rule against the AST.
func (r *InvalidJSONFormRule) Check(input rules.LintInput) []rules.Violation {
	if input.AST == nil || input.AST.AST == nil {
		return nil
	}

	meta := r.Metadata()
	var violations []rules.Violation

	for _, node := range input.AST.AST.Children {
		if node == nil {
			continue
		}

		vs := r.checkNode(node, input, meta)
		violations = append(violations, vs...)
	}

	return violations
}

// checkNode inspects a single AST node for invalid JSON form.
// Handles direct instructions, HEALTHCHECK, and ONBUILD wrappers.
func (r *InvalidJSONFormRule) checkNode(
	node *parser.Node,
	input rules.LintInput,
	meta rules.RuleMetadata,
) []rules.Violation {
	keyword := strings.ToLower(node.Value)

	switch {
	case jsonFormInstructions[keyword]:
		return r.checkInstruction(node, keyword, input, meta)

	case keyword == "healthcheck":
		return r.checkHealthcheck(node, input, meta)

	case keyword == "onbuild":
		return r.checkOnbuild(node, input, meta)
	}

	return nil
}

// checkInstruction checks a direct JSON-form instruction (CMD, RUN, SHELL, etc.).
func (r *InvalidJSONFormRule) checkInstruction(
	node *parser.Node,
	keyword string,
	input rules.LintInput,
	meta rules.RuleMetadata,
) []rules.Violation {
	// If BuildKit successfully parsed JSON, nothing to flag.
	if node.Attributes != nil && node.Attributes["json"] {
		return nil
	}

	argText := extractArgsText(node.Original)
	return r.buildViolation(node, keyword, argText, input, meta)
}

// checkHealthcheck handles HEALTHCHECK CMD [...] where the CMD sub-instruction
// may have invalid JSON.
func (r *InvalidJSONFormRule) checkHealthcheck(
	node *parser.Node,
	input rules.LintInput,
	meta rules.RuleMetadata,
) []rules.Violation {
	if node.Attributes != nil && node.Attributes["json"] {
		return nil
	}

	// HEALTHCHECK has "CMD" or "NONE" as the sub-type.
	if node.Next == nil || !strings.EqualFold(node.Next.Value, "CMD") {
		return nil
	}

	argText := extractHealthcheckArgs(node.Original)
	return r.buildViolation(node, "healthcheck cmd", argText, input, meta)
}

// checkOnbuild handles ONBUILD <instruction> [...] where the sub-instruction
// may have invalid JSON.
func (r *InvalidJSONFormRule) checkOnbuild(
	node *parser.Node,
	input rules.LintInput,
	meta rules.RuleMetadata,
) []rules.Violation {
	if node.Next == nil || len(node.Next.Children) == 0 || node.Next.Children[0] == nil {
		return nil
	}

	subNode := node.Next.Children[0]
	subKeyword := strings.ToLower(subNode.Value)

	if !jsonFormInstructions[subKeyword] {
		return nil
	}

	// Check the sub-instruction's attributes for JSON parse success.
	if subNode.Attributes != nil && subNode.Attributes["json"] {
		return nil
	}

	argText := extractOnbuildArgs(node.Original, subKeyword)
	return r.buildViolation(node, subKeyword, argText, input, meta)
}

// buildViolation creates a violation if argText looks like invalid JSON form.
// Returns nil if the argument text does not look like a malformed JSON array.
func (r *InvalidJSONFormRule) buildViolation(
	node *parser.Node,
	keyword, argText string,
	input rules.LintInput,
	meta rules.RuleMetadata,
) []rules.Violation {
	trimmed := strings.TrimSpace(argText)
	if !looksLikeInvalidJSON(trimmed) {
		return nil
	}

	msg := formatInvalidJSONMessage(keyword, trimmed)
	loc := rules.NewLocationFromRanges(input.File, node.Location())
	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL)

	// Attempt fix for single-line instructions only.
	if node.StartLine == node.EndLine {
		if fix := buildJSONFix(input.File, input.Source, node.StartLine, trimmed); fix != nil {
			v = v.WithSuggestedFix(fix)
		}
	}

	return []rules.Violation{v}
}

// looksLikeInvalidJSON returns true if the argument text starts with `[` but
// is not a shell test construct and contains a closing `]`.
func looksLikeInvalidJSON(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "[") {
		return false
	}
	// Bash [[ test syntax is not a JSON array attempt.
	if strings.HasPrefix(trimmed, "[[") {
		return false
	}
	// Must contain a closing bracket to look like an array attempt.
	if !strings.Contains(trimmed, "]") {
		return false
	}
	// POSIX single-bracket test: `[ expression ]`. These start with "[ "
	// and have no commas between brackets (JSON arrays use commas).
	if strings.HasPrefix(trimmed, "[ ") {
		closingIdx := strings.Index(trimmed, "]")
		if closingIdx > 0 && !strings.Contains(trimmed[1:closingIdx], ",") {
			return false
		}
	}
	return true
}

// extractArgsText strips the instruction keyword and flags from the original
// line, returning only the argument text.
//
// Example: "CMD --mount=type=cache [bash, -lc]" → "[bash, -lc]"
func extractArgsText(original string) string {
	rest := strings.TrimSpace(original)

	// Skip the instruction keyword.
	idx := strings.IndexFunc(rest, unicode.IsSpace)
	if idx < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[idx:])

	// Skip flags (--name or --name=value).
	for strings.HasPrefix(rest, "--") {
		end := strings.IndexFunc(rest, unicode.IsSpace)
		if end < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[end:])
	}

	return rest
}

// extractHealthcheckArgs extracts the argument text after "HEALTHCHECK [flags] CMD".
func extractHealthcheckArgs(original string) string {
	rest := strings.TrimSpace(original)

	// Skip "HEALTHCHECK".
	idx := strings.IndexFunc(rest, unicode.IsSpace)
	if idx < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[idx:])

	// Skip flags.
	for strings.HasPrefix(rest, "--") {
		end := strings.IndexFunc(rest, unicode.IsSpace)
		if end < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[end:])
	}

	// Skip "CMD" or "NONE".
	idx = strings.IndexFunc(rest, unicode.IsSpace)
	if idx < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[idx:])

	return rest
}

// extractOnbuildArgs extracts the argument text for the sub-instruction
// inside an ONBUILD line.
//
// Example: "ONBUILD CMD [bash, -lc]" → "[bash, -lc]"
func extractOnbuildArgs(original, subKeyword string) string {
	upper := strings.ToUpper(original)
	subUpper := strings.ToUpper(subKeyword)

	// Skip past "ONBUILD" before searching for the sub-instruction keyword.
	onbuildEnd := strings.Index(upper, "ONBUILD")
	if onbuildEnd < 0 {
		return ""
	}
	onbuildEnd += len("ONBUILD")

	idx := strings.Index(upper[onbuildEnd:], subUpper)
	if idx < 0 {
		return ""
	}
	rest := original[onbuildEnd+idx+len(subUpper):]
	rest = strings.TrimSpace(rest)

	// Skip flags.
	for strings.HasPrefix(rest, "--") {
		end := strings.IndexFunc(rest, unicode.IsSpace)
		if end < 0 {
			return ""
		}
		rest = strings.TrimSpace(rest[end:])
	}

	return rest
}

// formatInvalidJSONMessage produces the violation message.
func formatInvalidJSONMessage(keyword, argText string) string {
	upper := strings.ToUpper(keyword)

	// Truncate long argument text for the message.
	display := argText
	if len(display) > 60 {
		display = display[:57] + "..."
	}

	if strings.EqualFold(keyword, "shell") {
		return fmt.Sprintf(
			"%s requires valid JSON exec-form; %s is not valid JSON and will cause a build error",
			upper, display,
		)
	}

	return fmt.Sprintf(
		"invalid JSON in exec-form arguments for %s: %s",
		upper, display,
	)
}

// buildJSONFix attempts to repair the malformed JSON array and returns a
// SuggestedFix, or nil if the content cannot be repaired.
func buildJSONFix(file string, source []byte, line int, argText string) *rules.SuggestedFix {
	fixed, ok := tryRepairJSON(argText)
	if !ok {
		return nil
	}

	// Find the column range of the bracketed expression in the source line.
	lineIdx := line - 1 // Convert 1-based to 0-based.
	lines := bytes.Split(source, []byte("\n"))
	if lineIdx < 0 || lineIdx >= len(lines) {
		return nil
	}
	sourceLine := string(lines[lineIdx])

	startCol := strings.Index(sourceLine, "[")
	if startCol < 0 {
		return nil
	}

	// Find the last ']' on the line (the closing bracket).
	endCol := strings.LastIndex(sourceLine, "]")
	if endCol < 0 || endCol <= startCol {
		return nil
	}
	endCol++ // Exclusive end.

	return &rules.SuggestedFix{
		Description: "Fix JSON exec-form syntax",
		Safety:      rules.FixSuggestion,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(file, line, startCol, line, endCol),
				NewText:  fixed,
			},
		},
		IsPreferred: true,
	}
}

// tryRepairJSON attempts to fix common invalid JSON array patterns:
//   - Unquoted strings: [bash, -lc, "echo"] → ["bash", "-lc", "echo"]
//   - Single quotes: ['bash', '-lc'] → ["bash", "-lc"]
//   - Trailing comma: ["bash", "-lc",] → ["bash", "-lc"]
//
// Returns the fixed JSON string and true, or ("", false) if repair fails.
func tryRepairJSON(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return "", false
	}

	inner := trimmed[1 : len(trimmed)-1]
	inner = strings.TrimSpace(inner)

	// Remove trailing comma.
	inner = strings.TrimRight(inner, " \t")
	inner = strings.TrimSuffix(inner, ",")
	inner = strings.TrimRight(inner, " \t")

	if inner == "" {
		// Empty array [] — this should already be valid JSON, but be safe.
		return "[]", true
	}

	parts := splitJSONElements(inner)
	if len(parts) == 0 {
		return "", false
	}

	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		quoted := ensureDoubleQuoted(p)
		if quoted == "" {
			return "", false
		}
		result = append(result, quoted)
	}

	if len(result) == 0 {
		return "", false
	}

	fixed := "[" + strings.Join(result, ", ") + "]"

	// Validate: the repaired string must be a valid JSON string array.
	var check []string
	if err := json.Unmarshal([]byte(fixed), &check); err != nil {
		return "", false
	}

	return fixed, true
}

// splitJSONElements splits a comma-separated list, respecting double and single
// quoted strings so that commas inside quotes are not treated as delimiters.
// Uses rune iteration for UTF-8 safety.
func splitJSONElements(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	var quoteChar rune

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if inQuote {
			current.WriteRune(ch)
			if ch == '\\' && i+1 < len(runes) {
				i++
				current.WriteRune(runes[i])
				continue
			}
			if ch == quoteChar {
				inQuote = false
			}
			continue
		}

		switch ch {
		case '"', '\'':
			inQuote = true
			quoteChar = ch
			current.WriteRune(ch)
		case ',':
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}

	// Add the last element.
	if current.Len() > 0 || len(parts) > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// ensureDoubleQuoted wraps a value in double quotes if it isn't already.
// Converts single-quoted values to double-quoted. Returns "" if the value
// contains characters that can't be safely quoted.
func ensureDoubleQuoted(s string) string {
	if s == "" {
		return ""
	}

	// Already double-quoted — validate and keep as-is.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s
	}

	// Single-quoted — convert to double-quoted.
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		inner := s[1 : len(s)-1]
		// Escape any double quotes inside.
		inner = strings.ReplaceAll(inner, `"`, `\"`)
		return `"` + inner + `"`
	}

	// Unquoted — wrap in double quotes, escaping internal double quotes and backslashes.
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func init() {
	rules.Register(NewInvalidJSONFormRule())
}
