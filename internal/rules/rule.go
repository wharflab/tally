package rules

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// BuildContext provides optional build-time context for rules.
// This is nil in v1.0 but allows rules to be context-aware in the future.
type BuildContext struct {
	// ContextDir is the path to the build context directory (optional).
	ContextDir string

	// BuildArgs are --build-arg values passed to the build (optional).
	BuildArgs map[string]string

	// Future fields for context-aware linting (post-v1.0):
	// - DockerIgnorePatterns []string
	// - ContextFiles map[string]bool (file existence cache)
	// - RegistryClient interface{}
}

// LintInput contains all the information a rule needs to check a Dockerfile.
// Rules should work with the AST and typed instructions, not raw source text.
//
// The linter guarantees that AST and Source are always valid (non-nil) when
// Check is called. If parsing fails, the linter reports parse errors and
// exits without invoking any rules (following ESLint's approach).
//
// IMPORTANT: LintInput is read-only. Rules must not mutate any fields (File,
// AST, Stages, MetaArgs, Source, Context, Config). If a rule needs to modify
// data, it must copy it first. This prevents hidden coupling between rules.
type LintInput struct {
	// File is the path to the Dockerfile being linted.
	File string

	// AST is the parsed Dockerfile AST from BuildKit (guaranteed non-nil).
	// Use AST nodes for line information, not raw source counting.
	AST *parser.Result

	// Stages contains the parsed build stages with typed instructions.
	// This is populated by BuildKit's instructions.Parse().
	Stages []instructions.Stage

	// MetaArgs contains ARG instructions that appear before the first FROM.
	// These are global build arguments that affect base image selection.
	MetaArgs []instructions.ArgCommand

	// Source is the raw source content of the Dockerfile.
	// Used for snippet extraction and directive parsing.
	Source []byte

	// Context is optional build context (nil in v1.0).
	Context *BuildContext

	// Config is the rule-specific configuration (type depends on rule).
	Config any
}

// RuleMetadata contains static information about a rule.
type RuleMetadata struct {
	// Code is the unique identifier (e.g., "DL3006", "max-lines").
	Code string

	// Name is the human-readable rule name.
	Name string

	// Description explains what the rule checks.
	Description string

	// DocURL links to detailed documentation.
	DocURL string

	// DefaultSeverity is the severity when not overridden.
	DefaultSeverity Severity

	// Category groups related rules (e.g., "security", "performance", "style").
	Category string

	// EnabledByDefault indicates if the rule runs without explicit opt-in.
	EnabledByDefault bool

	// IsExperimental marks rules that may change or be removed.
	IsExperimental bool
}

// Rule is the interface that all linting rules must implement.
type Rule interface {
	// Metadata returns static information about the rule.
	Metadata() RuleMetadata

	// Check runs the rule against the given input and returns any violations.
	// The AST and Source fields are guaranteed non-nil. The Context field
	// may be nil in v1.0 (context-aware linting is optional).
	Check(input LintInput) []Violation
}

// ConfigurableRule is an optional interface for rules that accept configuration.
type ConfigurableRule interface {
	Rule

	// DefaultConfig returns the default configuration for this rule.
	DefaultConfig() any

	// ValidateConfig checks if a configuration is valid for this rule.
	ValidateConfig(config any) error
}
