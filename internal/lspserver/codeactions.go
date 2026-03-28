package lspserver

import (
	"context"
	"log"
	"strings"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"

	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/rules"
)

// codeActionsForDocument returns quick-fix code actions for the given range.
func (s *Server) codeActionsForDocument(
	ctx context.Context,
	doc *Document,
	params *protocol.CodeActionParams,
) []protocol.CodeAction {
	includeQuickFix := true
	includeFixAll := true
	if params.Context.Only != nil {
		includeQuickFix = kindRequested(params.Context.Only, protocol.CodeActionKindQuickFix)
		includeFixAll = kindRequested(params.Context.Only, fixAllCodeActionKind)
	}

	if !includeQuickFix && !includeFixAll {
		return nil
	}

	// Use cached lint results from publishDiagnostics when the version matches.
	violations, ok := s.lintCache.get(doc.URI, doc.Version)
	if !ok {
		violations = s.lintContent(ctx, doc.URI, []byte(doc.Content))
		if s.documentVersionCurrent(doc.URI, doc.Version) {
			s.lintCache.set(doc.URI, doc.Version, violations)
		}
	}

	actions := make([]protocol.CodeAction, 0, len(violations)+1)

	if includeQuickFix {
		resolveFn := func(sf *rules.SuggestedFix) []rules.TextEdit {
			return resolveFixEdits(ctx, doc, sf)
		}
		actions = append(actions, quickFixActions(violations, params, resolveFn)...)
	}

	if includeFixAll {
		if action := s.fixAllCodeAction(doc, violations); action != nil {
			actions = append(actions, *action)
		}
	}

	return actions
}

// resolveFixEdits resolves async fix edits on-the-fly using the registered resolver.
// Returns nil if the resolver is not available or resolution fails.
func resolveFixEdits(ctx context.Context, doc *Document, suggestedFix *rules.SuggestedFix) []rules.TextEdit {
	resolver := fix.GetResolver(suggestedFix.ResolverID)
	if resolver == nil {
		return nil
	}

	resolveCtx := fix.ResolveContext{
		FilePath: uriToPath(doc.URI),
		Content:  []byte(doc.Content),
	}

	edits, err := resolver.Resolve(ctx, resolveCtx, suggestedFix)
	if err != nil {
		log.Printf("lsp: code action resolve failed for %s: %v", suggestedFix.ResolverID, err)
		return nil
	}
	return edits
}

// quickFixActions builds one CodeAction per fix alternative for each violation.
// resolveFn is called for fixes with NeedsResolve=true; pass nil to skip async fixes.
func quickFixActions(
	violations []rules.Violation,
	params *protocol.CodeActionParams,
	resolveFn func(*rules.SuggestedFix) []rules.TextEdit,
) []protocol.CodeAction {
	var actions []protocol.CodeAction

	for _, v := range violations {
		fixes := v.AllFixes()
		if len(fixes) == 0 {
			continue
		}

		vRange := violationRange(v)
		if !rangesOverlap(vRange, params.Range) {
			continue
		}

		var ctxDiags []*protocol.Diagnostic
		if params.Context != nil {
			ctxDiags = params.Context.Diagnostics
		}
		matchedDiags := matchingDiagnostics(v, ctxDiags)
		preferred := v.PreferredFix()

		for _, sf := range fixes {
			fixEdits := sf.Edits
			if sf.NeedsResolve {
				if resolveFn == nil {
					continue
				}
				resolved := resolveFn(sf)
				if resolved == nil {
					continue
				}
				fixEdits = resolved
			}

			if len(fixEdits) == 0 {
				continue
			}

			edits := convertTextEdits(fixEdits)
			if len(edits) == 0 {
				continue
			}

			// Only the preferred fix is marked IsPreferred in the LSP sense.
			// For single-fix violations, the fix is preferred when explicitly
			// marked or when it is safe.
			isPreferred := sf == preferred && (sf.IsPreferred || sf.Safety == rules.FixSafe)

			action := protocol.CodeAction{
				Title:       sf.Description,
				Kind:        ptrTo(protocol.CodeActionKindQuickFix),
				IsPreferred: &isPreferred,
				Diagnostics: &matchedDiags,
				Edit: &protocol.WorkspaceEdit{
					Changes: new(map[protocol.DocumentUri][]*protocol.TextEdit{
						params.TextDocument.Uri: edits,
					}),
				},
			}
			actions = append(actions, action)
		}
	}

	return actions
}

func kindRequested(only *[]protocol.CodeActionKind, kind protocol.CodeActionKind) bool {
	if only == nil {
		return true
	}
	for _, requested := range *only {
		if requested == kind {
			return true
		}
		if requested != "" && strings.HasPrefix(string(kind), string(requested)+".") {
			return true
		}
	}
	return false
}

// convertTextEdits converts tally TextEdits to LSP TextEdits.
func convertTextEdits(edits []rules.TextEdit) []*protocol.TextEdit {
	result := make([]*protocol.TextEdit, 0, len(edits))
	for _, e := range edits {
		loc := e.Location
		if loc.IsFileLevel() {
			continue
		}

		startLine := clampUint32(loc.Start.Line - 1)
		startChar := clampUint32(loc.Start.Column)
		endLine := startLine
		endChar := startChar

		if !loc.IsPointLocation() {
			endLine = clampUint32(loc.End.Line - 1)
			endChar = clampUint32(loc.End.Column)
		}

		result = append(result, &protocol.TextEdit{
			Range: protocol.Range{
				Start: protocol.Position{Line: startLine, Character: startChar},
				End:   protocol.Position{Line: endLine, Character: endChar},
			},
			NewText: e.NewText,
		})
	}
	return result
}

// rangesOverlap checks if two LSP ranges overlap.
// LSP ranges are half-open [start, end), so touching ranges (a.End == b.Start)
// are not considered overlapping.
func rangesOverlap(a, b protocol.Range) bool {
	if a.End.Line < b.Start.Line || (a.End.Line == b.Start.Line && a.End.Character <= b.Start.Character) {
		return false
	}
	if b.End.Line < a.Start.Line || (b.End.Line == a.Start.Line && b.End.Character <= a.Start.Character) {
		return false
	}
	return true
}

// matchingDiagnostics finds diagnostics that match a violation by message and range.
func matchingDiagnostics(v rules.Violation, diagnostics []*protocol.Diagnostic) []*protocol.Diagnostic {
	vRange := violationRange(v)
	var matched []*protocol.Diagnostic
	for _, d := range diagnostics {
		if d.Range.Start.Line == vRange.Start.Line && d.Message == v.Message {
			matched = append(matched, d)
		}
	}
	return matched
}
