package tally

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/syntax"
)

// InvalidOnbuildTriggerRuleCode is the full rule code.
const InvalidOnbuildTriggerRuleCode = rules.TallyRulePrefix + "invalid-onbuild-trigger"

// forbiddenOnbuildTriggers are already caught by hadolint/DL3043.
var forbiddenOnbuildTriggers = map[string]bool{
	command.From:       true,
	command.Onbuild:    true,
	command.Maintainer: true,
}

// InvalidOnbuildTriggerRule detects unknown or misspelled instruction keywords
// used as ONBUILD triggers (e.g. ONBUILD COPPY . /app).
//
// The top-level syntax check (tally/unknown-instruction) only inspects the
// outer instruction token, which is always "ONBUILD" for these nodes. This
// rule performs the complementary check on the trigger keyword itself.
type InvalidOnbuildTriggerRule struct{}

// NewInvalidOnbuildTriggerRule creates a new invalid-onbuild-trigger rule instance.
func NewInvalidOnbuildTriggerRule() *InvalidOnbuildTriggerRule {
	return &InvalidOnbuildTriggerRule{}
}

// Metadata returns the rule metadata.
func (r *InvalidOnbuildTriggerRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            InvalidOnbuildTriggerRuleCode,
		Name:            "Invalid ONBUILD Trigger",
		Description:     "ONBUILD trigger instruction is not a valid Dockerfile instruction",
		DocURL:          rules.TallyDocURL(InvalidOnbuildTriggerRuleCode),
		DefaultSeverity: rules.SeverityError,
		Category:        "correctness",
		IsExperimental:  false,
	}
}

// Check runs the invalid-onbuild-trigger rule against the AST.
func (r *InvalidOnbuildTriggerRule) Check(input rules.LintInput) []rules.Violation {
	if input.AST == nil || input.AST.AST == nil {
		return nil
	}

	meta := r.Metadata()
	var violations []rules.Violation

	for _, node := range input.AST.AST.Children {
		if node == nil || !strings.EqualFold(node.Value, command.Onbuild) {
			continue
		}

		trigger := onbuildTrigger(node)
		if trigger == "" {
			continue
		}

		lower := strings.ToLower(trigger)

		// Skip forbidden triggers already reported by hadolint/DL3043.
		if forbiddenOnbuildTriggers[lower] {
			continue
		}

		// Valid instruction — nothing to report.
		if _, ok := command.Commands[lower]; ok {
			continue
		}

		loc := rules.NewLineLocation(input.File, node.StartLine)
		suggestion := syntax.ClosestInstruction(trigger)

		var msg string
		if suggestion != "" {
			msg = fmt.Sprintf(
				"unknown instruction %q in ONBUILD trigger (did you mean %q?)",
				strings.ToUpper(trigger),
				strings.ToUpper(suggestion),
			)
		} else {
			msg = fmt.Sprintf("unknown instruction %q in ONBUILD trigger", strings.ToUpper(trigger))
		}

		v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
			WithDocURL(meta.DocURL)

		if suggestion != "" {
			if fix := buildTriggerFix(input.File, input.Source, node, trigger, suggestion); fix != nil {
				v = v.WithSuggestedFix(fix)
			}
		}

		violations = append(violations, v)
	}

	return violations
}

// onbuildTrigger extracts the trigger keyword from an ONBUILD AST node.
// BuildKit represents ONBUILD as: node{Value:"onbuild"} → node.Next.Children[0]{Value:"trigger"}
func onbuildTrigger(node *parser.Node) string {
	if node.Next == nil || len(node.Next.Children) == 0 || node.Next.Children[0] == nil {
		return ""
	}
	return node.Next.Children[0].Value
}

// buildTriggerFix creates a TextEdit that replaces the misspelled trigger keyword
// with the suggested correction. Returns nil if the position cannot be determined.
func buildTriggerFix(file string, source []byte, node *parser.Node, typo, suggestion string) *rules.SuggestedFix {
	lineIdx := node.StartLine - 1 // 0-based
	lines := bytes.Split(source, []byte("\n"))
	if lineIdx < 0 || lineIdx >= len(lines) {
		return nil
	}
	line := string(lines[lineIdx])

	startCol, endCol := triggerColumnRange(line, typo)
	if startCol < 0 {
		return nil
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Replace %q with %q", strings.ToUpper(typo), strings.ToUpper(suggestion)),
		Safety:      rules.FixSuggestion,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(file, node.StartLine, startCol, node.StartLine, endCol),
				NewText:  strings.ToUpper(suggestion),
			},
		},
		IsPreferred: true,
	}
}

// triggerColumnRange finds the 0-based [start, end) column range of the trigger
// keyword in a source line such as "ONBUILD COPPY . /app".
// Returns (-1, -1) if not found.
// triggerColumnRange finds the 0-based [start, end) column range of the trigger
// keyword in a source line such as "ONBUILD COPPY . /app".
// Returns (-1, -1) if not found.
//
// Note: BuildKit's parser does not populate column info on ONBUILD sub-nodes
// (Location() returns zeros), so we derive the position from the source text.
// The search is case-insensitive and handles indented ONBUILD instructions.
func triggerColumnRange(line, trigger string) (int, int) {
	upper := strings.ToUpper(line)
	prefix := strings.ToUpper(command.Onbuild)

	// Find where ONBUILD starts (handles leading whitespace / indentation).
	idx := strings.Index(upper, prefix)
	if idx < 0 {
		return -1, -1
	}

	// Scan past "ONBUILD" and any whitespace.
	i := idx + len(prefix)
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}

	// The next word should be the trigger keyword.
	upperTrigger := strings.ToUpper(trigger)
	remaining := strings.ToUpper(line[i:])
	if !strings.HasPrefix(remaining, upperTrigger) {
		return -1, -1
	}
	return i, i + len(trigger)
}

func init() {
	rules.Register(NewInvalidOnbuildTriggerRule())
}
