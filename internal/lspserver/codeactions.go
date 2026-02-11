package lspserver

import (
	"strings"

	protocol "github.com/tinovyatkin/tally/internal/lsp/protocol"

	"github.com/tinovyatkin/tally/internal/rules"
)

// codeActionsForDocument returns quick-fix code actions for the given range.
func (s *Server) codeActionsForDocument(
	doc *Document,
	params *protocol.CodeActionParams,
) []protocol.CodeAction {
	includeQuickFix := true
	includeFixAll := true
	if params.Context.Only != nil {
		includeQuickFix = kindRequested(params.Context.Only, protocol.CodeActionKindQuickFix)
		includeFixAll = kindRequested(params.Context.Only, fixAllCodeActionKind)
	}

	// Use cached lint results from publishDiagnostics when the version matches.
	violations, ok := s.lintCache.get(doc.URI, doc.Version)
	if !ok {
		violations = s.lintContent(doc.URI, []byte(doc.Content))
	}

	actions := make([]protocol.CodeAction, 0, len(violations)+1)

	if includeQuickFix {
		for _, v := range violations {
			if v.SuggestedFix == nil || v.SuggestedFix.NeedsResolve {
				continue
			}
			if len(v.SuggestedFix.Edits) == 0 {
				continue
			}

			vRange := violationRange(v)
			if !rangesOverlap(vRange, params.Range) {
				continue
			}

			edits := convertTextEdits(v.SuggestedFix.Edits)
			if len(edits) == 0 {
				continue
			}

			matchedDiags := matchingDiagnostics(v, params.Context.Diagnostics)
			action := protocol.CodeAction{
				Title:       v.SuggestedFix.Description,
				Kind:        ptrTo(protocol.CodeActionKindQuickFix),
				IsPreferred: new(v.SuggestedFix.IsPreferred || v.SuggestedFix.Safety == rules.FixSafe),
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

	if includeFixAll {
		if action := s.fixAllCodeAction(doc); action != nil {
			actions = append(actions, *action)
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
