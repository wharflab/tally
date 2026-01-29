// Package processor provides a composable violation processing pipeline.
//
// The processor chain pattern is inspired by golangci-lint's approach:
// violations flow through a sequence of processors, each transforming
// the slice (filtering, modifying, or augmenting).
//
// Standard pipeline order:
//  1. PathNormalization - Cross-platform path consistency
//  2. EnableFilter - Remove violations for disabled rules
//  3. SeverityOverride - Apply config severity overrides
//  4. PathExclusionFilter - Remove per-rule path exclusions
//  5. InlineDirectiveFilter - Apply # tally ignore=... etc.
//  6. Deduplication - Remove duplicate violations
//  7. Sorting - Stable output ordering
//  8. SnippetAttachment - Populate SourceCode field
package processor

import (
	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

// Processor transforms a slice of violations.
// Implementations should be stateless where possible, using Context for shared state.
type Processor interface {
	// Name returns the processor's identifier (for debugging/logging).
	Name() string

	// Process applies the processor's logic to violations.
	// Returns the transformed slice (may be same, filtered, or modified).
	// Must not modify the input slice; return a new slice if filtering.
	Process(violations []rules.Violation, ctx *Context) []rules.Violation
}

// Context provides shared state for processors.
// Populated once before running the chain, then passed to each processor.
type Context struct {
	// Config is the loaded configuration.
	Config *config.Config

	// FileSources maps file paths to their raw source content.
	// Used by SnippetAttachment for extracting source code.
	FileSources map[string][]byte

	// SourceMaps caches parsed source maps by file path.
	// Lazily populated by GetSourceMap.
	sourceMaps map[string]*sourcemap.SourceMap
}

// NewContext creates a new processor context.
func NewContext(cfg *config.Config, fileSources map[string][]byte) *Context {
	return &Context{
		Config:      cfg,
		FileSources: fileSources,
		sourceMaps:  make(map[string]*sourcemap.SourceMap),
	}
}

// GetSourceMap returns or creates a SourceMap for the given file.
// Returns nil if the file is not in FileSources.
func (ctx *Context) GetSourceMap(file string) *sourcemap.SourceMap {
	if sm, ok := ctx.sourceMaps[file]; ok {
		return sm
	}
	source, ok := ctx.FileSources[file]
	if !ok {
		return nil
	}
	sm := sourcemap.New(source)
	ctx.sourceMaps[file] = sm
	return sm
}

// Chain runs processors in sequence.
type Chain struct {
	processors []Processor
}

// NewChain creates a new processor chain.
func NewChain(processors ...Processor) *Chain {
	return &Chain{processors: processors}
}

// Process runs all processors in sequence.
func (c *Chain) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
	for _, p := range c.processors {
		violations = p.Process(violations, ctx)
	}
	return violations
}

// filterViolations is a helper for processors that filter violations.
// It returns a new slice containing only violations where keep() returns true.
func filterViolations(violations []rules.Violation, keep func(v rules.Violation) bool) []rules.Violation {
	result := make([]rules.Violation, 0, len(violations))
	for _, v := range violations {
		if keep(v) {
			result = append(result, v)
		}
	}
	return result
}

// transformViolations is a helper for processors that modify violations.
// It returns a new slice with each violation transformed by transform().
func transformViolations(
	violations []rules.Violation,
	transform func(v rules.Violation) rules.Violation,
) []rules.Violation {
	result := make([]rules.Violation, len(violations))
	for i, v := range violations {
		result[i] = transform(v)
	}
	return result
}
