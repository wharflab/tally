package lspserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"
	"github.com/wharflab/tally/internal/rules"
)

func TestConvertTextEdits(t *testing.T) {
	t.Parallel()

	edits := []rules.TextEdit{
		{
			Location: rules.NewRangeLocation("Dockerfile", 5, 4, 5, 7),
			NewText:  "apt-get",
		},
	}

	lspEdits := convertTextEdits(edits)
	require.Len(t, lspEdits, 1)
	// 1-based line 5 → 0-based line 4
	assert.Equal(t, uint32(4), lspEdits[0].Range.Start.Line)
	assert.Equal(t, uint32(4), lspEdits[0].Range.Start.Character)
}

func TestConvertTextEdits_SkipsFileLevel(t *testing.T) {
	t.Parallel()

	edits := []rules.TextEdit{
		{
			Location: rules.NewFileLocation("Dockerfile"),
			NewText:  "whole file",
		},
	}

	lspEdits := convertTextEdits(edits)
	assert.Empty(t, lspEdits)
}

func TestMatchingDiagnostics(t *testing.T) {
	t.Parallel()

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 3, 0, 3, 10),
		"test-rule",
		"test message",
		rules.SeverityWarning,
	)

	diags := []*protocol.Diagnostic{
		{
			Range: protocol.Range{
				Start: protocol.Position{Line: 2, Character: 0}, // 0-based line 2 = 1-based line 3
				End:   protocol.Position{Line: 2, Character: 10},
			},
			Message: "test message",
		},
		{
			Range: protocol.Range{
				Start: protocol.Position{Line: 5, Character: 0},
				End:   protocol.Position{Line: 5, Character: 10},
			},
			Message: "other message",
		},
	}

	matched := matchingDiagnostics(v, diags)
	assert.Len(t, matched, 1)
	assert.Equal(t, "test message", matched[0].Message)
}

func TestCodeActions_MultiFix(t *testing.T) {
	t.Parallel()

	loc := rules.NewRangeLocation("Dockerfile", 5, 0, 5, 20)
	fixA := &rules.SuggestedFix{
		Description: "Comment out the line",
		Safety:      rules.FixSafe,
		IsPreferred: true,
		Edits:       []rules.TextEdit{{Location: loc, NewText: "# commented: STOPSIGNAL SIGKILL"}},
	}
	fixB := &rules.SuggestedFix{
		Description: "Delete the line",
		Safety:      rules.FixSuggestion,
		Edits:       []rules.TextEdit{{Location: loc, NewText: ""}},
	}

	v := rules.NewViolation(loc, "tally/windows/no-stopsignal", "STOPSIGNAL not supported", rules.SeverityWarning).
		WithSuggestedFixes([]*rules.SuggestedFix{fixA, fixB})

	violations := []rules.Violation{v}
	uri := protocol.DocumentUri("file:///test/Dockerfile")

	requestRange := protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: 100, Character: 0},
	}

	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{Uri: uri},
		Range:        requestRange,
		Context: &protocol.CodeActionContext{
			Diagnostics: []*protocol.Diagnostic{
				{
					Range: protocol.Range{
						Start: protocol.Position{Line: 4, Character: 0},
						End:   protocol.Position{Line: 4, Character: 20},
					},
					Message: "STOPSIGNAL not supported",
				},
			},
		},
	}

	// Manually build code actions using the same logic as codeActionsForDocument
	// but without needing a full server setup.
	actions := buildQuickFixActions(violations, params)

	require.Len(t, actions, 2, "should emit one code action per fix alternative")

	// First action: preferred fix (Comment out)
	assert.Equal(t, "Comment out the line", actions[0].Title)
	assert.True(t, *actions[0].IsPreferred, "preferred safe fix should be IsPreferred=true")

	// Second action: non-preferred fix (Delete)
	assert.Equal(t, "Delete the line", actions[1].Title)
	assert.False(t, *actions[1].IsPreferred, "non-preferred suggestion fix should be IsPreferred=false")
}

func TestCodeActions_SingleFix_BackwardCompat(t *testing.T) {
	t.Parallel()

	loc := rules.NewRangeLocation("Dockerfile", 3, 0, 3, 3)
	v := rules.NewViolation(loc, "DL3027", "do not use apt", rules.SeverityWarning).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Replace 'apt' with 'apt-get'",
			Safety:      rules.FixSafe,
			Edits:       []rules.TextEdit{{Location: loc, NewText: "apt-get"}},
		})

	violations := []rules.Violation{v}
	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{Uri: "file:///test/Dockerfile"},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 100, Character: 0},
		},
		Context: &protocol.CodeActionContext{
			Diagnostics: []*protocol.Diagnostic{
				{
					Range: protocol.Range{
						Start: protocol.Position{Line: 2, Character: 0},
						End:   protocol.Position{Line: 2, Character: 3},
					},
					Message: "do not use apt",
				},
			},
		},
	}

	actions := buildQuickFixActions(violations, params)
	require.Len(t, actions, 1, "single fix should produce one action")
	assert.Equal(t, "Replace 'apt' with 'apt-get'", actions[0].Title)
	assert.True(t, *actions[0].IsPreferred, "safe fix should be preferred")
}

func TestCodeActions_MultiFix_DifferentSafety(t *testing.T) {
	t.Parallel()

	loc := rules.NewRangeLocation("Dockerfile", 2, 0, 2, 25)
	fixSafe := &rules.SuggestedFix{
		Description: "Set ENV COMPOSER_NO_INTERACTION=1",
		Safety:      rules.FixSafe,
		IsPreferred: true,
		Edits:       []rules.TextEdit{{Location: loc, NewText: "ENV COMPOSER_NO_INTERACTION=1\nRUN composer install"}},
	}
	fixUnsafe := &rules.SuggestedFix{
		Description: "Add --no-interaction flag",
		Safety:      rules.FixUnsafe,
		Edits:       []rules.TextEdit{{Location: loc, NewText: "RUN composer install --no-interaction"}},
	}

	v := rules.NewViolation(loc, "tally/php/composer-no-interaction", "msg", rules.SeverityWarning).
		WithSuggestedFixes([]*rules.SuggestedFix{fixSafe, fixUnsafe})

	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{Uri: "file:///test/Dockerfile"},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 100, Character: 0},
		},
		Context: &protocol.CodeActionContext{},
	}

	actions := buildQuickFixActions([]rules.Violation{v}, params)
	require.Len(t, actions, 2)

	// Safe preferred fix
	assert.Equal(t, "Set ENV COMPOSER_NO_INTERACTION=1", actions[0].Title)
	assert.True(t, *actions[0].IsPreferred)

	// Unsafe alternative
	assert.Equal(t, "Add --no-interaction flag", actions[1].Title)
	assert.False(t, *actions[1].IsPreferred)
}

func TestCodeActions_NoFixes(t *testing.T) {
	t.Parallel()

	loc := rules.NewRangeLocation("Dockerfile", 1, 0, 1, 10)
	v := rules.NewViolation(loc, "tally/no-multi-spaces", "msg", rules.SeverityWarning)

	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{Uri: "file:///test/Dockerfile"},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 100, Character: 0},
		},
		Context: &protocol.CodeActionContext{},
	}

	actions := buildQuickFixActions([]rules.Violation{v}, params)
	assert.Empty(t, actions)
}

// buildQuickFixActions extracts the quick-fix code action logic for unit testing
// without needing a full LSP server.
func buildQuickFixActions(violations []rules.Violation, params *protocol.CodeActionParams) []protocol.CodeAction {
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
			if len(fixEdits) == 0 {
				continue
			}

			edits := convertTextEdits(fixEdits)
			if len(edits) == 0 {
				continue
			}

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
