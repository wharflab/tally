package fix

import (
	"bytes"
	"context"

	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

// newlineResolver implements FixResolver for newline-between-instructions fixes.
// It re-parses the modified content and generates all needed blank-line edits,
// ensuring correct positions even after other fixes have changed line structure.
type newlineResolver struct{}

// ID returns the resolver identifier.
func (r *newlineResolver) ID() string {
	return rules.NewlineResolverID
}

// Resolve re-parses the current content and generates all blank-line edits.
// Because this resolver always produces the complete set of edits for the whole file,
// only the first invocation per file generates edits; subsequent calls find the content
// already correct and return nil.
func (r *newlineResolver) Resolve(_ context.Context, resolveCtx ResolveContext, fix *rules.SuggestedFix) ([]rules.TextEdit, error) {
	data, ok := fix.ResolverData.(*rules.NewlineResolveData)
	if !ok {
		return nil, nil // Skip silently if data is wrong type
	}

	// Parse the modified content
	dockerfile, err := parser.Parse(bytes.NewReader(resolveCtx.Content))
	if err != nil {
		return nil, nil //nolint:nilerr // Skip silently - don't fail fix process
	}

	children := dockerfile.AST.Children
	if len(children) < 2 {
		return nil, nil
	}

	sm := sourcemap.New(resolveCtx.Content)
	var edits []rules.TextEdit

	for i := 1; i < len(children); i++ {
		prev := children[i-1]
		curr := children[i]

		prevEndLine := sm.ResolveEndLine(prev.EndLine)
		result := sm.ComputeNewlineGap(prevEndLine, curr.StartLine, prev.Value, curr.Value, curr.PrevComment, data.Mode)
		if result.Skip || result.Gap == result.WantGap {
			continue
		}

		currEffectiveStart := sm.EffectiveStartLine(curr.StartLine, curr.PrevComment)

		if result.Gap < result.WantGap {
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(resolveCtx.FilePath, prevEndLine+1, 0, prevEndLine+1, 0),
				NewText:  "\n",
			})
		} else {
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(resolveCtx.FilePath, prevEndLine+1+result.WantGap, 0, currEffectiveStart, 0),
				NewText:  "",
			})
		}
	}

	return edits, nil
}

// init registers the newline resolver.
func init() {
	RegisterResolver(&newlineResolver{})
}
