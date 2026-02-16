package linter

import "github.com/wharflab/tally/internal/processor"

// CLIProcessors returns the standard CLI processor chain and the inline directive
// filter (the caller needs it for [processor.InlineDirectiveFilter.AdditionalViolations]).
func CLIProcessors() (*processor.Chain, *processor.InlineDirectiveFilter) {
	inlineFilter := processor.NewInlineDirectiveFilter()
	chain := processor.NewChain(
		processor.NewPathNormalization(),   // Normalize paths for cross-platform consistency
		processor.NewSeverityOverride(),    // Apply severity overrides (must run before EnableFilter)
		processor.NewEnableFilter(),        // Filter rules with severity="off"
		processor.NewPathExclusionFilter(), // Apply per-rule path exclusions
		inlineFilter,                       // Apply inline ignore directives
		processor.NewSupersession(),        // Drop lower-severity when error exists
		processor.NewDeduplication(),       // Remove duplicate violations
		processor.NewSorting(),             // Stable output ordering
		processor.NewSnippetAttachment(),   // Attach source code snippets
	)
	return chain, inlineFilter
}

// LSPProcessors returns the LSP processor chain.
// The LSP chain omits path normalization, path exclusion, and snippet attachment
// since those are CLI-specific concerns.
func LSPProcessors() *processor.Chain {
	return processor.NewChain(
		processor.NewSeverityOverride(),
		processor.NewEnableFilter(),
		processor.NewInlineDirectiveFilter(),
		processor.NewSupersession(),
		processor.NewDeduplication(),
		processor.NewSorting(),
	)
}
