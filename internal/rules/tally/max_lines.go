// Package tally implements tally-specific linting rules for Dockerfiles.
package tally

import (
	"fmt"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
)

// MaxLinesConfig is the configuration for the max-lines rule.
//
// Default: 50 lines (excluding blanks and comments).
// This was determined by analyzing 500 public Dockerfiles on GitHub:
// P90 = 53 lines. With skip-blank-lines and skip-comments enabled by default,
// this provides a comfortable margin while flagging unusually long Dockerfiles.
//
// Pointer types are used for fields that need tri-state semantics (unset vs explicit-zero).
type MaxLinesConfig struct {
	// Max is the maximum number of lines allowed (0 = disabled, nil = use default).
	Max *int `json:"max,omitempty" jsonschema:"description=Maximum number of lines allowed (0 = disabled),default=50,minimum=0"`

	// SkipBlankLines excludes blank lines from the count (nil = use default).
	SkipBlankLines *bool `json:"skip-blank-lines,omitempty" jsonschema:"description=Exclude blank lines from the count,default=true"`

	// SkipComments excludes comment lines from the count (nil = use default).
	SkipComments *bool `json:"skip-comments,omitempty" jsonschema:"description=Exclude comment lines from the count,default=true"`
}

// DefaultMaxLinesConfig returns the default configuration.
func DefaultMaxLinesConfig() MaxLinesConfig {
	maxLines := 50
	skipBlankLines := true
	skipComments := true
	return MaxLinesConfig{
		Max:            &maxLines,       // P90 of 500 analyzed Dockerfiles
		SkipBlankLines: &skipBlankLines, // Count only meaningful lines
		SkipComments:   &skipComments,   // Count only instruction lines
	}
}

// MaxLinesRule implements the max-lines linting rule.
type MaxLinesRule struct{}

// NewMaxLinesRule creates a new max-lines rule instance.
func NewMaxLinesRule() *MaxLinesRule {
	return &MaxLinesRule{}
}

// Metadata returns the rule metadata.
func (r *MaxLinesRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.TallyRulePrefix + "max-lines",
		Name:            "Maximum Lines",
		Description:     "Limits the maximum number of lines in a Dockerfile",
		DocURL:          "https://github.com/tinovyatkin/tally/blob/main/docs/rules/max-lines.md",
		DefaultSeverity: rules.SeverityError,
		Category:        "maintainability",
		IsExperimental:  false,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
// Follows ESLint's meta.schema pattern for rule options validation.
// Supports either an integer shorthand (just max) or full object config.
func (r *MaxLinesRule) Schema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"oneOf": []any{
			map[string]any{
				"type":    "integer",
				"minimum": 0,
			},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"max": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"default":     50,
						"description": "Maximum number of lines allowed (0 = disabled)",
					},
					"skip-blank-lines": map[string]any{
						"type":        "boolean",
						"default":     true,
						"description": "Exclude blank lines from the count",
					},
					"skip-comments": map[string]any{
						"type":        "boolean",
						"default":     true,
						"description": "Exclude comment lines from the count",
					},
				},
				"additionalProperties": false,
			},
		},
	}
}

// Check runs the max-lines rule using the AST for accurate line counting.
// Like ESLint's max-lines, it uses parsed AST data for comments rather than
// naive string matching.
func (r *MaxLinesRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)

	// Rule is disabled if Max is nil or 0
	if cfg.Max == nil || *cfg.Max <= 0 {
		return nil
	}
	maxLines := *cfg.Max

	// Get total lines from the AST root node's EndLine
	// This gives us the last line of actual content
	totalLines := getTotalLines(input.AST)

	count := totalLines

	// Subtract comment lines if configured (using AST's PrevComment data)
	// nil defaults to true (skip comments)
	skipComments := cfg.SkipComments == nil || *cfg.SkipComments
	if skipComments {
		count -= countCommentLines(input.AST.AST)
	}

	// Handle blank lines
	// nil defaults to true (skip blank lines)
	skipBlankLines := cfg.SkipBlankLines == nil || *cfg.SkipBlankLines
	if skipBlankLines {
		count -= countBlankLines(input.AST)
	} else if count <= maxLines {
		// Trailing blanks only matter if not already over limit
		count += countTrailingBlanks(input.Source)
	}

	if count > maxLines {
		// Report from the first line exceeding the limit (like ESLint)
		// With 1-based line numbers, line (Max+1) is the first line over
		return []rules.Violation{
			rules.NewViolation(
				rules.NewLineLocation(input.File, maxLines+1), // 1-based: first line over max
				r.Metadata().Code,
				fmt.Sprintf("file has %d lines, maximum allowed is %d", count, maxLines),
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL),
		}
	}

	return nil
}

// getTotalLines returns the total number of lines from the AST.
// AST is guaranteed non-nil by the linter contract.
func getTotalLines(ast *parser.Result) int {
	return ast.AST.EndLine
}

// countTrailingBlanks counts blank lines after the last instruction.
// A single trailing \n is just a line terminator; additional \n's are blank lines.
func countTrailingBlanks(source []byte) int {
	count := 0
	for i := len(source) - 1; i >= 0 && source[i] == '\n'; i-- {
		count++
	}
	// First newline is line terminator, rest are blank lines
	if count > 1 {
		return count - 1
	}
	return 0
}

// countCommentLines counts lines that are comment-only using AST data.
// BuildKit stores comments in Node.PrevComment for each node.
func countCommentLines(node *parser.Node) int {
	if node == nil {
		return 0
	}

	count := 0

	// Count comments attached to this node
	count += len(node.PrevComment)

	// Recursively count in children
	for _, child := range node.Children {
		count += countCommentLines(child)
	}

	// Count in Next chain
	if node.Next != nil {
		count += countCommentLines(node.Next)
	}

	return count
}

// countBlankLines counts lines that have no AST node content.
// A blank line is one that has neither code nor comments.
// AST is guaranteed non-nil by the linter contract.
func countBlankLines(ast *parser.Result) int {
	totalLines := ast.AST.EndLine
	if totalLines <= 0 {
		return 0
	}

	// Track which lines are occupied (have code or comments)
	occupied := make(map[int]bool)

	// The root node spans the entire file - process its children only
	// Root comments (file header comments)
	numRootComments := len(ast.AST.PrevComment)
	for i := range numRootComments {
		occupied[1+i] = true // Root comments start at line 1
	}

	// Process child instructions (the root node's Children contains instructions)
	for _, child := range ast.AST.Children {
		markOccupiedLines(child, occupied)
	}

	// Process Next chain (alternative structure for instructions)
	if ast.AST.Next != nil {
		markOccupiedLines(ast.AST.Next, occupied)
	}

	// Count unoccupied lines
	blankCount := 0
	for line := 1; line <= totalLines; line++ {
		if !occupied[line] {
			blankCount++
		}
	}
	return blankCount
}

// markOccupiedLines recursively marks all lines that have AST content.
func markOccupiedLines(node *parser.Node, occupied map[int]bool) {
	if node == nil {
		return
	}

	// Mark lines covered by this node's instruction
	for line := node.StartLine; line <= node.EndLine; line++ {
		occupied[line] = true
	}

	// Mark lines for preceding comments
	// Each PrevComment entry represents one comment line before StartLine
	numComments := len(node.PrevComment)
	for i := range numComments {
		occupied[node.StartLine-numComments+i] = true
	}

	// Recurse into children
	for _, child := range node.Children {
		markOccupiedLines(child, occupied)
	}

	// Recurse into Next chain (sibling instructions)
	if node.Next != nil {
		markOccupiedLines(node.Next, occupied)
	}
}

// DefaultConfig returns the default configuration for this rule.
func (r *MaxLinesRule) DefaultConfig() any {
	return DefaultMaxLinesConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *MaxLinesRule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

// resolveConfig extracts the MaxLinesConfig from input, falling back to defaults.
// Supports integer shorthand (just max value) or full object config.
func (r *MaxLinesRule) resolveConfig(config any) MaxLinesConfig {
	switch v := config.(type) {
	case int:
		// Integer shorthand: just the max value with default booleans
		defaults := DefaultMaxLinesConfig()
		defaults.Max = &v
		return defaults
	case float64:
		// JSON numbers come as float64
		maxVal := int(v)
		defaults := DefaultMaxLinesConfig()
		defaults.Max = &maxVal
		return defaults
	}
	return configutil.Coerce(config, DefaultMaxLinesConfig())
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewMaxLinesRule())
}
