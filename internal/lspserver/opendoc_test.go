package lspserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"
	"github.com/wharflab/tally/internal/rules"
)

func TestShowDocActions_WithDocURL(t *testing.T) {
	t.Parallel()

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 3, 0, 3, 10),
		"tally/max-lines",
		"file too long",
		rules.SeverityWarning,
	)
	v.DocURL = "https://wharflab.github.io/tally/rules/tally/max-lines/"

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})
	actions := showDocActions([]rules.Violation{v}, params)

	require.Len(t, actions, 1)
	assert.Equal(t, "Show documentation for tally/max-lines", actions[0].Title)
	assert.Nil(t, actions[0].Kind, "show-doc actions should have nil Kind")
	require.NotNil(t, actions[0].Command)
	assert.Equal(t, openRuleDocCommand, actions[0].Command.Command)
	require.NotNil(t, actions[0].Command.Arguments)
	args, ok := (*actions[0].Command.Arguments)[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "https://wharflab.github.io/tally/rules/tally/max-lines/", args["url"])
}

func TestShowDocActions_NoDocURL(t *testing.T) {
	t.Parallel()

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 3, 0, 3, 10),
		"tally/test-rule",
		"msg",
		rules.SeverityWarning,
	)
	// No DocURL set.

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})
	actions := showDocActions([]rules.Violation{v}, params)

	assert.Empty(t, actions)
}

func TestShowDocActions_Dedup(t *testing.T) {
	t.Parallel()

	loc1 := rules.NewRangeLocation("Dockerfile", 3, 0, 3, 10)
	loc2 := rules.NewRangeLocation("Dockerfile", 5, 0, 5, 10)

	v1 := rules.NewViolation(loc1, "tally/max-lines", "msg1", rules.SeverityWarning)
	v1.DocURL = "https://example.com/doc"
	v2 := rules.NewViolation(loc2, "tally/max-lines", "msg2", rules.SeverityWarning)
	v2.DocURL = "https://example.com/doc"

	params := makeCodeActionParams("file:///test/Dockerfile", fullRange(), &protocol.CodeActionContext{})
	actions := showDocActions([]rules.Violation{v1, v2}, params)

	require.Len(t, actions, 1, "should deduplicate by ruleCode")
}

func TestShowDocActions_OutOfRange(t *testing.T) {
	t.Parallel()

	v := rules.NewViolation(
		rules.NewRangeLocation("Dockerfile", 50, 0, 50, 10),
		"tally/max-lines",
		"msg",
		rules.SeverityWarning,
	)
	v.DocURL = "https://example.com/doc"

	// Request range is lines 0-5 only.
	params := makeCodeActionParams("file:///test/Dockerfile",
		protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: 5, Character: 0},
		},
		&protocol.CodeActionContext{},
	)
	actions := showDocActions([]rules.Violation{v}, params)

	assert.Empty(t, actions, "should not emit action for violations outside the requested range")
}

func TestParseOpenRuleDocArgs(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		args := []any{map[string]any{"url": "https://example.com"}}
		assert.Equal(t, "https://example.com", parseOpenRuleDocArgs(&args))
	})

	t.Run("nil args", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, parseOpenRuleDocArgs(nil))
	})

	t.Run("empty args", func(t *testing.T) {
		t.Parallel()
		args := []any{}
		assert.Empty(t, parseOpenRuleDocArgs(&args))
	})

	t.Run("wrong type", func(t *testing.T) {
		t.Parallel()
		args := []any{"not a map"}
		assert.Empty(t, parseOpenRuleDocArgs(&args))
	})
}
