package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NewlineBetweenInstructionsRuleCode is the full rule code for the newline-between-instructions rule.
const NewlineBetweenInstructionsRuleCode = rules.TallyRulePrefix + "newline-between-instructions"

// NewlineBetweenInstructionsConfig is the configuration for the newline-between-instructions rule.
type NewlineBetweenInstructionsConfig struct {
	// Mode controls blank-line behavior between instructions.
	// "grouped": same instruction types grouped (no blank), different types separated (blank line).
	// "always": every instruction followed by a blank line.
	// "never": no blank lines between instructions.
	Mode string `json:"mode,omitempty"`
}

// DefaultNewlineBetweenInstructionsConfig returns the default configuration.
func DefaultNewlineBetweenInstructionsConfig() NewlineBetweenInstructionsConfig {
	return NewlineBetweenInstructionsConfig{
		Mode: "grouped",
	}
}

// NewlineBetweenInstructionsRule implements the newline-between-instructions linting rule.
type NewlineBetweenInstructionsRule struct{}

// NewNewlineBetweenInstructionsRule creates a new newline-between-instructions rule instance.
func NewNewlineBetweenInstructionsRule() *NewlineBetweenInstructionsRule {
	return &NewlineBetweenInstructionsRule{}
}

// Metadata returns the rule metadata.
func (r *NewlineBetweenInstructionsRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NewlineBetweenInstructionsRuleCode,
		Name:            "Newline Between Instructions",
		Description:     "Controls blank lines between Dockerfile instructions",
		DocURL:          rules.TallyDocURL(NewlineBetweenInstructionsRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		FixPriority:     55,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
// Supports string shorthand ("grouped", "always", "never") or full object config.
func (r *NewlineBetweenInstructionsRule) Schema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"oneOf": []any{
			map[string]any{
				"type": "string",
				"enum": []any{"grouped", "always", "never"},
			},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"mode": map[string]any{
						"type":        "string",
						"enum":        []any{"grouped", "always", "never"},
						"default":     "grouped",
						"description": "Blank-line mode: grouped, always, or never",
					},
				},
				"additionalProperties": false,
			},
		},
	}
}

// DefaultConfig returns the default configuration for this rule.
func (r *NewlineBetweenInstructionsRule) DefaultConfig() any {
	return DefaultNewlineBetweenInstructionsConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *NewlineBetweenInstructionsRule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

// Check runs the newline-between-instructions rule.
func (r *NewlineBetweenInstructionsRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	meta := r.Metadata()
	sm := input.SourceMap()

	children := input.AST.AST.Children
	if len(children) < 2 {
		return nil
	}

	var violations []rules.Violation

	for i := 1; i < len(children); i++ {
		prev := children[i-1]
		curr := children[i]

		// Compute end line of previous instruction (including heredocs and continuations).
		// Node.EndLine includes heredoc body; resolveEndLine only uses Location().End.Line
		// which excludes heredocs. Use Node.EndLine as the base and scan for continuations.
		prevEndLine := resolveNodeEndLine(sm, prev)

		// Compute start of current instruction accounting for attached comments.
		// BuildKit stores preceding comments in PrevComment; those lines sit
		// directly above StartLine.
		currEffectiveStart := curr.StartLine - len(curr.PrevComment)

		gap := currEffectiveStart - prevEndLine - 1

		var wantGap int
		switch cfg.Mode {
		case "always":
			if gap >= 1 {
				continue // already has at least one blank line
			}
			wantGap = 1
		case "never":
			wantGap = 0
		default: // "grouped"
			if strings.EqualFold(prev.Value, curr.Value) {
				wantGap = 0
			} else {
				wantGap = 1
			}
		}

		if gap == wantGap {
			continue
		}

		// Build violation message and fix.
		var message string
		var edits []rules.TextEdit

		if gap < wantGap {
			// Need to insert a blank line.
			message = fmt.Sprintf(
				"expected blank line between %s and %s",
				strings.ToUpper(prev.Value), strings.ToUpper(curr.Value),
			)
			edits = []rules.TextEdit{{
				Location: rules.NewRangeLocation(input.File, prevEndLine+1, 0, prevEndLine+1, 0),
				NewText:  "\n",
			}}
		} else {
			// Need to remove blank line(s).
			message = fmt.Sprintf(
				"unexpected blank line between %s and %s",
				strings.ToUpper(prev.Value), strings.ToUpper(curr.Value),
			)
			edits = []rules.TextEdit{{
				Location: rules.NewRangeLocation(input.File, prevEndLine+1, 0, currEffectiveStart, 0),
				NewText:  "",
			}}
		}

		loc := rules.NewLineLocation(input.File, currEffectiveStart)
		v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithSuggestedFix(&rules.SuggestedFix{
				Description: "Fix blank lines between instructions",
				Safety:      rules.FixSafe,
				Priority:    meta.FixPriority,
				Edits:       edits,
				IsPreferred: true,
			})
		violations = append(violations, v)
	}

	return violations
}

// resolveConfig extracts the config from input, falling back to defaults.
// Supports string shorthand (just mode) or full object config.
func (r *NewlineBetweenInstructionsRule) resolveConfig(config any) NewlineBetweenInstructionsConfig {
	if v, ok := config.(string); ok {
		return NewlineBetweenInstructionsConfig{Mode: v}
	}
	return configutil.Coerce(config, DefaultNewlineBetweenInstructionsConfig())
}

// resolveNodeEndLine returns the last line of an instruction, including heredoc
// bodies and backslash continuations. Node.EndLine includes heredocs but
// resolveEndLine (which uses Location().End.Line) may not; this helper uses
// Node.EndLine as the base and scans forward for continuations.
func resolveNodeEndLine(sm *sourcemap.SourceMap, node *parser.Node) int {
	endLine := node.EndLine
	endLine = min(endLine, sm.LineCount())
	for l := endLine; l <= sm.LineCount(); l++ {
		line := sm.Line(l - 1) // l is 1-based, sm.Line is 0-based
		if !strings.HasSuffix(strings.TrimRight(line, " \t"), `\`) {
			return l
		}
		endLine = min(l+1, sm.LineCount())
	}
	return endLine
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewNewlineBetweenInstructionsRule())
}
