// Package maxlines implements the max-lines rule for Dockerfile linting.
package maxlines

import (
	"fmt"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Config is the configuration for the max-lines rule.
//
// Default: 50 lines (excluding blanks and comments).
// This was determined by analyzing 500 public Dockerfiles on GitHub:
// P90 = 53 lines. With skip-blank-lines and skip-comments enabled by default,
// this provides a comfortable margin while flagging unusually long Dockerfiles.
type Config struct {
	// Max is the maximum number of lines allowed (0 = disabled).
	// Default: 50 (P90 of 500 analyzed Dockerfiles, counting only code lines).
	Max int

	// SkipBlankLines excludes blank lines from the count.
	// Default: true (count only meaningful lines).
	SkipBlankLines bool

	// SkipComments excludes comment lines from the count.
	// Default: true (count only instruction lines).
	SkipComments bool
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Max:            50,   // P90 of 500 analyzed Dockerfiles
		SkipBlankLines: true, // Count only meaningful lines
		SkipComments:   true, // Count only instruction lines
	}
}

// Rule implements the max-lines linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:             "max-lines",
		Name:             "Maximum Lines",
		Description:      "Limits the maximum number of lines in a Dockerfile",
		DocURL:           "https://github.com/tinovyatkin/tally/blob/main/docs/rules/max-lines.md",
		DefaultSeverity:  rules.SeverityError,
		Category:         "maintainability",
		EnabledByDefault: true, // Enabled with sensible defaults
		IsExperimental:   false,
	}
}

// Check runs the max-lines rule using the AST for accurate line counting.
// Like ESLint's max-lines, it uses parsed AST data for comments rather than
// naive string matching.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)

	// Rule is disabled if Max is 0
	if cfg.Max <= 0 {
		return nil
	}

	// Get total lines from the AST root node's EndLine
	// This gives us the last line of actual content
	totalLines := getTotalLines(input.AST)

	count := totalLines

	// Subtract comment lines if configured (using AST's PrevComment data)
	if cfg.SkipComments {
		count -= countCommentLines(input.AST.AST)
	}

	// Handle blank lines
	if cfg.SkipBlankLines {
		count -= countBlankLines(input.AST)
	} else if count <= cfg.Max {
		// Trailing blanks only matter if not already over limit
		count += countTrailingBlanks(input.Source)
	}

	if count > cfg.Max {
		// Report from the first line exceeding the limit (like ESLint)
		// With 1-based line numbers, line (Max+1) is the first line over
		return []rules.Violation{
			rules.NewViolation(
				rules.NewLineLocation(input.File, cfg.Max+1), // 1-based: first line over max
				r.Metadata().Code,
				fmt.Sprintf("file has %d lines, maximum allowed is %d", count, cfg.Max),
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
func (r *Rule) DefaultConfig() any {
	return DefaultConfig()
}

// ValidateConfig checks if the configuration is valid.
func (r *Rule) ValidateConfig(config any) error {
	if config == nil {
		return nil
	}
	var cfg Config
	switch v := config.(type) {
	case Config:
		cfg = v
	case *Config:
		if v == nil {
			return nil
		}
		cfg = *v
	default:
		return fmt.Errorf("expected Config, got %T", config)
	}
	if cfg.Max < 0 {
		return fmt.Errorf("max must be >= 0, got %d", cfg.Max)
	}
	return nil
}

// resolveConfig extracts the Config from input, falling back to defaults.
func (r *Rule) resolveConfig(config any) Config {
	if config == nil {
		return DefaultConfig()
	}
	if cfg, ok := config.(Config); ok {
		return cfg
	}
	// Try pointer
	if cfg, ok := config.(*Config); ok && cfg != nil {
		return *cfg
	}
	return DefaultConfig()
}

// New creates a new max-lines rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
