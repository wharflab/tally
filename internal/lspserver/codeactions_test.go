package lspserver

import (
	"context"
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

func TestCodeActionsForDocument_FiltersInvocationContextsByRequestedRange(t *testing.T) {
	t.Parallel()

	s := New()
	content := "FROM alpine\nRUN apt-get update\nRUN echo later\n"
	uri := protocol.DocumentUri("file:///test/Dockerfile")
	doc := &Document{
		URI:     string(uri),
		Version: 1,
		Content: content,
	}

	selectedLoc := rules.NewRangeLocation("/test/Dockerfile", 2, 0, 2, 18)
	selected := rules.NewViolation(selectedLoc, "tally/selected-rule", "selected msg", rules.SeverityWarning).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Fix selected violation",
			Safety:      rules.FixSafe,
			Edits:       []rules.TextEdit{{Location: selectedLoc, NewText: "RUN apt-get update && true"}},
		})
	selected.InvocationKey = "bake://target:selected"

	otherLoc := rules.NewRangeLocation("/test/Dockerfile", 3, 0, 3, 14)
	other := rules.NewViolation(otherLoc, "tally/other-rule", "other msg", rules.SeverityWarning).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Fix unrelated violation",
			Safety:      rules.FixSafe,
			Edits:       []rules.TextEdit{{Location: otherLoc, NewText: "RUN echo fixed"}},
		})
	other.InvocationKey = "bake://target:other"

	parseResult := parseDockerfile(t, []byte(content))
	s.lintCache.set(doc.URI, doc.Version, []rules.Violation{selected, other}, nil, parseResult)

	params := makeCodeActionParams(
		uri,
		protocol.Range{
			Start: protocol.Position{Line: 1, Character: 0},
			End:   protocol.Position{Line: 1, Character: 18},
		},
		&protocol.CodeActionContext{},
	)

	actions := s.codeActionsForDocument(context.Background(), doc, params)
	titles := codeActionTitles(actions)

	assert.Contains(t, titles, "Fix selected violation")
	assert.Contains(t, titles, "Fix all auto-fixable issues")
	assert.Contains(t, titles, "Suppress tally/selected-rule for this line")
	assert.Contains(t, titles, "Suppress tally/selected-rule for this file")
	assert.NotContains(t, titles, "Fix unrelated violation")
	assert.NotContains(t, titles, "Suppress tally/other-rule for this line")
}

func TestFilterViolationsForParams_IncludesCursorWithinViolation(t *testing.T) {
	t.Parallel()

	first := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 2, 0, 2, 18),
		"tally/first-rule",
		"first msg",
		rules.SeverityWarning,
	)
	first.InvocationKey = "bake://target:first"

	second := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 4, 0, 4, 12),
		"tally/second-rule",
		"second msg",
		rules.SeverityWarning,
	)
	second.InvocationKey = "bake://target:second"

	params := makeCodeActionParams(
		"file:///test/Dockerfile",
		protocol.Range{
			Start: protocol.Position{Line: 1, Character: 5},
			End:   protocol.Position{Line: 1, Character: 5},
		},
		&protocol.CodeActionContext{},
	)

	filtered := filterViolationsForParams([]rules.Violation{first, second}, params)

	require.Len(t, filtered, 1)
	assert.Equal(t, "tally/first-rule", filtered[0].RuleCode)
	assert.True(t, hasMultipleInvocationContexts([]rules.Violation{first, second}))
	assert.False(t, hasMultipleInvocationContexts(filtered))
}

func TestQuickFixActions_MultiFix(t *testing.T) {
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

	uri := protocol.DocumentUri("file:///project/Dockerfile")
	params := makeCodeActionParams(
		uri,
		fullRange(),
		&protocol.CodeActionContext{
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
	)

	actions := quickFixActions([]rules.Violation{v}, params, nil)

	require.Len(t, actions, 2, "should emit one code action per fix alternative")

	// First action: preferred fix (Comment out)
	assert.Equal(t, "Comment out the line", actions[0].Title)
	assert.True(t, *actions[0].IsPreferred, "preferred safe fix should be IsPreferred=true")

	// Second action: non-preferred fix (Delete)
	assert.Equal(t, "Delete the line", actions[1].Title)
	assert.False(t, *actions[1].IsPreferred, "non-preferred suggestion fix should be IsPreferred=false")
}

func TestQuickFixActions_SingleFix_BackwardCompat(t *testing.T) {
	t.Parallel()

	loc := rules.NewRangeLocation("Dockerfile", 3, 0, 3, 3)
	v := rules.NewViolation(loc, "DL3027", "do not use apt", rules.SeverityWarning).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Replace 'apt' with 'apt-get'",
			Safety:      rules.FixSafe,
			Edits:       []rules.TextEdit{{Location: loc, NewText: "apt-get"}},
		})

	params := makeCodeActionParams(
		"file:///test/Dockerfile",
		fullRange(),
		&protocol.CodeActionContext{
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
	)

	actions := quickFixActions([]rules.Violation{v}, params, nil)
	require.Len(t, actions, 1, "single fix should produce one action")
	assert.Equal(t, "Replace 'apt' with 'apt-get'", actions[0].Title)
	assert.True(t, *actions[0].IsPreferred, "safe fix should be preferred")
}

func TestQuickFixActions_MultiFix_DifferentSafety(t *testing.T) {
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

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})

	actions := quickFixActions([]rules.Violation{v}, params, nil)
	require.Len(t, actions, 2)

	assert.Equal(t, "Set ENV COMPOSER_NO_INTERACTION=1", actions[0].Title)
	assert.True(t, *actions[0].IsPreferred)

	assert.Equal(t, "Add --no-interaction flag", actions[1].Title)
	assert.False(t, *actions[1].IsPreferred)
}

func TestQuickFixActions_NoFixes(t *testing.T) {
	t.Parallel()

	loc := rules.NewRangeLocation("Dockerfile", 1, 0, 1, 10)
	v := rules.NewViolation(loc, "tally/no-multi-spaces", "msg", rules.SeverityWarning)

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})

	actions := quickFixActions([]rules.Violation{v}, params, nil)
	assert.Empty(t, actions)
}

func TestQuickFixActions_NeedsResolve(t *testing.T) {
	t.Parallel()

	loc := rules.NewRangeLocation("Dockerfile", 2, 0, 2, 20)
	resolvedEdits := []rules.TextEdit{{Location: loc, NewText: "resolved content"}}

	sf := &rules.SuggestedFix{
		Description:  "Async fix",
		Safety:       rules.FixSafe,
		NeedsResolve: true,
		ResolverID:   "test-resolver",
	}

	v := rules.NewViolation(loc, "tally/test-rule", "msg", rules.SeverityWarning).
		WithSuggestedFix(sf)

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})

	t.Run("resolved successfully", func(t *testing.T) {
		t.Parallel()
		resolveFn := func(fix *rules.SuggestedFix) []rules.TextEdit {
			return resolvedEdits
		}
		actions := quickFixActions([]rules.Violation{v}, params, resolveFn)
		require.Len(t, actions, 1)
		assert.Equal(t, "Async fix", actions[0].Title)
		assert.True(t, *actions[0].IsPreferred)
	})

	t.Run("resolver returns nil skips fix", func(t *testing.T) {
		t.Parallel()
		resolveFn := func(fix *rules.SuggestedFix) []rules.TextEdit {
			return nil
		}
		actions := quickFixActions([]rules.Violation{v}, params, resolveFn)
		assert.Empty(t, actions)
	})

	t.Run("nil resolveFn skips async fix", func(t *testing.T) {
		t.Parallel()
		actions := quickFixActions([]rules.Violation{v}, params, nil)
		assert.Empty(t, actions)
	})
}

func TestQuickFixActions_MultiFix_SuggestionPreferred_StillMarked(t *testing.T) {
	t.Parallel()

	// Regression: when a multi-fix violation has a FixSuggestion as the preferred
	// fix (without explicit IsPreferred), the IDE should still highlight it as
	// preferred so the user knows which alternative to pick.
	loc := rules.NewRangeLocation("Dockerfile", 3, 0, 3, 15)
	v := rules.NewViolation(loc, "tally/test-rule", "msg", rules.SeverityWarning).
		WithSuggestedFixes([]*rules.SuggestedFix{
			{Description: "Suggestion fix", Safety: rules.FixSuggestion, Edits: []rules.TextEdit{{Location: loc, NewText: "a"}}},
			{Description: "Unsafe fix", Safety: rules.FixUnsafe, Edits: []rules.TextEdit{{Location: loc, NewText: "b"}}},
		})

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})
	actions := quickFixActions([]rules.Violation{v}, params, nil)

	require.Len(t, actions, 2)
	// The first alternative is the preferred fix — it must be marked even though it's FixSuggestion
	assert.True(t, *actions[0].IsPreferred, "preferred fix among alternatives should be IsPreferred")
	assert.False(t, *actions[1].IsPreferred)
}

func TestQuickFixActions_SingleFix_UnsafeNotPreferred(t *testing.T) {
	t.Parallel()

	// Single-fix FixUnsafe without IsPreferred should NOT be auto-preferred by the IDE.
	// This preserves the pre-existing behavior.
	loc := rules.NewRangeLocation("Dockerfile", 2, 0, 2, 10)
	v := rules.NewViolation(loc, "tally/test-rule", "msg", rules.SeverityWarning).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Risky fix",
			Safety:      rules.FixUnsafe,
			Edits:       []rules.TextEdit{{Location: loc, NewText: "x"}},
		})

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})
	actions := quickFixActions([]rules.Violation{v}, params, nil)

	require.Len(t, actions, 1)
	assert.False(t, *actions[0].IsPreferred, "single unsafe fix should not be auto-preferred")
}

func TestQuickFixActions_SingleFix_SuggestionNotPreferred(t *testing.T) {
	t.Parallel()

	// Single-fix FixSuggestion without explicit IsPreferred should NOT be
	// auto-preferred. This preserves the pre-existing behavior: the IDE
	// should not auto-apply a suggestion-level fix without user confirmation.
	loc := rules.NewRangeLocation("Dockerfile", 3, 0, 3, 10)
	v := rules.NewViolation(loc, "hadolint/DL3001", "msg", rules.SeverityWarning).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Comment out line",
			Safety:      rules.FixSuggestion,
			Edits:       []rules.TextEdit{{Location: loc, NewText: "# ..."}},
		})

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})
	actions := quickFixActions([]rules.Violation{v}, params, nil)

	require.Len(t, actions, 1)
	assert.False(t, *actions[0].IsPreferred, "single FixSuggestion without IsPreferred should not be auto-preferred")
}

func TestQuickFixActions_SingleFix_SuggestionExplicitlyPreferred(t *testing.T) {
	t.Parallel()

	// Single-fix FixSuggestion WITH IsPreferred: true should be preferred.
	// Rules like no-ungraceful-stopsignal use this pattern.
	loc := rules.NewRangeLocation("Dockerfile", 3, 0, 3, 10)
	v := rules.NewViolation(loc, "tally/no-ungraceful-stopsignal", "msg", rules.SeverityWarning).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Replace SIGKILL with SIGTERM",
			Safety:      rules.FixSuggestion,
			IsPreferred: true,
			Edits:       []rules.TextEdit{{Location: loc, NewText: "STOPSIGNAL SIGTERM"}},
		})

	params := makeCodeActionParams("file:///app/Dockerfile", fullRange(), &protocol.CodeActionContext{})
	actions := quickFixActions([]rules.Violation{v}, params, nil)

	require.Len(t, actions, 1)
	assert.True(t, *actions[0].IsPreferred, "single FixSuggestion with IsPreferred should be preferred")
}

// makeCodeActionParams builds a CodeActionParams for testing.
func makeCodeActionParams(uri protocol.DocumentUri, r protocol.Range, ctx *protocol.CodeActionContext) *protocol.CodeActionParams {
	return &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{Uri: uri},
		Range:        r,
		Context:      ctx,
	}
}

// fullRange returns an LSP range covering lines 0–100.
func fullRange() protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: 100, Character: 0},
	}
}

func codeActionTitles(actions []protocol.CodeAction) []string {
	titles := make([]string, 0, len(actions))
	for _, action := range actions {
		titles = append(titles, action.Title)
	}
	return titles
}
