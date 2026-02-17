package lspserver

import (
	"context"
	"encoding/json/jsontext"
	"path/filepath"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/jsonrpc2"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"
	"github.com/wharflab/tally/internal/rules"
)

func TestViolationRangeConversion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		location rules.Location
		expected protocol.Range
	}{
		{
			name:     "file-level",
			location: rules.NewFileLocation("test"),
			expected: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			},
		},
		{
			name:     "line 1 col 0 (point)",
			location: rules.NewLineLocation("test", 1),
			expected: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 1000},
			},
		},
		{
			name:     "range",
			location: rules.NewRangeLocation("test", 3, 5, 3, 15),
			expected: protocol.Range{
				Start: protocol.Position{Line: 2, Character: 5},
				End:   protocol.Position{Line: 2, Character: 15},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			v := rules.Violation{Location: tt.location}
			got := violationRange(v)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSeverityConversion(t *testing.T) {
	t.Parallel()
	snaps.WithConfig(
		snaps.JSON(snaps.JSONConfig{
			SortKeys: true,
			Indent:   " ",
		}),
	).MatchStandaloneJSON(t, map[string]protocol.DiagnosticSeverity{
		"error":   severityToLSP(rules.SeverityError),
		"warning": severityToLSP(rules.SeverityWarning),
		"info":    severityToLSP(rules.SeverityInfo),
		"style":   severityToLSP(rules.SeverityStyle),
	})
}

func TestURIToPath(t *testing.T) {
	t.Parallel()
	path := uriToPath("file:///tmp/Dockerfile")
	assert.Equal(t, filepath.FromSlash("/tmp/Dockerfile"), path)
}

func TestCancelPreempter_HandlesCancelRequest(t *testing.T) {
	t.Parallel()

	// With conn=nil and missing/invalid ID, Cancel is never called.
	p := &cancelPreempter{conn: nil}

	// Missing "id" field — params.ID stays nil, id.IsValid() is false, Cancel skipped.
	req := &jsonrpc2.Request{
		Method: "$/cancelRequest",
		Params: jsontext.Value(`{}`),
	}
	result, err := p.Preempt(context.Background(), req)
	assert.Nil(t, result)
	require.NoError(t, err, "malformed $/cancelRequest should not return an error")

	// Unrecognized ID type (bool) — silently ignored.
	req2 := &jsonrpc2.Request{
		Method: "$/cancelRequest",
		Params: jsontext.Value(`{"id":true}`),
	}
	result, err = p.Preempt(context.Background(), req2)
	assert.Nil(t, result)
	require.NoError(t, err, "unrecognized ID type should be silently ignored")

	// Unparseable JSON — silently ignored.
	req3 := &jsonrpc2.Request{
		Method: "$/cancelRequest",
		Params: jsontext.Value(`not-json`),
	}
	result, err = p.Preempt(context.Background(), req3)
	assert.Nil(t, result)
	require.NoError(t, err, "invalid JSON should be silently ignored")
}

func TestCancelPreempter_PassesThroughOtherMethods(t *testing.T) {
	t.Parallel()

	p := &cancelPreempter{conn: nil}

	req := &jsonrpc2.Request{
		Method: "textDocument/didOpen",
		Params: jsontext.Value(`{}`),
	}
	result, err := p.Preempt(context.Background(), req)
	assert.Nil(t, result)
	require.ErrorIs(t, err, jsonrpc2.ErrNotHandled)
}
