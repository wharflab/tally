package processor

import (
	"github.com/tinovyatkin/tally/internal/rules"
)

// SnippetAttachment populates the SourceCode field of violations.
// This extracts the relevant source code snippet for each violation location,
// enabling reporters to display context without re-parsing files.
type SnippetAttachment struct{}

// NewSnippetAttachment creates a new snippet attachment processor.
func NewSnippetAttachment() *SnippetAttachment {
	return &SnippetAttachment{}
}

// Name returns the processor's identifier.
func (p *SnippetAttachment) Name() string {
	return "snippet-attachment"
}

// Process attaches source code snippets to violations.
// Skips violations that already have SourceCode set or where the file
// is not in the context's FileSources.
func (p *SnippetAttachment) Process(violations []rules.Violation, ctx *Context) []rules.Violation {
	return transformViolations(violations, func(v rules.Violation) rules.Violation {
		// Skip if already has snippet
		if v.SourceCode != "" {
			return v
		}

		// Skip file-level violations (no specific line)
		if v.Location.IsFileLevel() {
			return v
		}

		// Get source map for the file
		sm := ctx.GetSourceMap(v.Location.File)
		if sm == nil {
			return v
		}

		// Extract snippet for the location
		v.SourceCode = extractSnippet(sm, v.Location)
		return v
	})
}

// extractSnippet extracts source code for a location.
// Location uses 1-based line numbers; SourceMap uses 0-based.
func extractSnippet(sm interface {
	Line(lineNum int) string
	Snippet(startLine, endLine int) string
}, loc rules.Location,
) string {
	if loc.IsPointLocation() {
		if loc.Start.Line < 1 {
			return ""
		}
		return sm.Line(loc.Start.Line - 1)
	}

	// Range: get all lines from start to end
	endLine := loc.End.Line
	if loc.End.Column == 0 && endLine > loc.Start.Line {
		endLine--
	}
	if loc.Start.Line < 1 || endLine < 1 {
		return ""
	}
	return sm.Snippet(loc.Start.Line-1, endLine-1)
}
