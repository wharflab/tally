package lspserver

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"path/filepath"
	"testing"
	"time"

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

	t.Run("file URI", func(t *testing.T) {
		t.Parallel()
		path := uriToPath("file:///tmp/Dockerfile")
		assert.Equal(t, filepath.FromSlash("/tmp/Dockerfile"), path)
	})

	t.Run("untitled URI returns synthetic path", func(t *testing.T) {
		t.Parallel()
		path := uriToPath("untitled:Untitled-1")
		assert.True(t, filepath.IsAbs(path), "untitled URI should resolve to an absolute path")
		assert.Equal(t, "Dockerfile", filepath.Base(path))
	})

	t.Run("vscode-notebook URI returns synthetic path", func(t *testing.T) {
		t.Parallel()
		path := uriToPath("vscode-notebook-cell://authority/path")
		assert.True(t, filepath.IsAbs(path))
		assert.Equal(t, "Dockerfile", filepath.Base(path))
	})
}

func TestIsVirtualURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		uri  string
		want bool
	}{
		{"untitled:Untitled-1", true},
		{"untitled://Untitled-1", true},
		{"vscode-notebook-cell://authority/path", true},
		{"file:///tmp/Dockerfile", false},
		{"/tmp/Dockerfile", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isVirtualURI(tt.uri), "isVirtualURI(%q)", tt.uri)
		})
	}
}

func TestCancelPreempter_HandlesCancelRequest(t *testing.T) {
	t.Parallel()

	s := New()
	p := &cancelPreempter{server: s}

	// Missing "id" field — params.ID stays nil, id.IsValid() is false, Cancel skipped.
	req := &jsonrpc2.Request{
		Method: string(protocol.MethodCancelRequest),
		Params: jsontext.Value(`{}`),
	}
	result, err := p.Preempt(context.Background(), req)
	assert.Nil(t, result)
	require.NoError(t, err, "malformed $/cancelRequest should not return an error")

	// Unrecognized ID type (bool) — silently ignored.
	req2 := &jsonrpc2.Request{
		Method: string(protocol.MethodCancelRequest),
		Params: jsontext.Value(`{"id":true}`),
	}
	result, err = p.Preempt(context.Background(), req2)
	assert.Nil(t, result)
	require.NoError(t, err, "unrecognized ID type should be silently ignored")

	// Unparseable JSON — silently ignored.
	req3 := &jsonrpc2.Request{
		Method: string(protocol.MethodCancelRequest),
		Params: jsontext.Value(`not-json`),
	}
	result, err = p.Preempt(context.Background(), req3)
	assert.Nil(t, result)
	require.NoError(t, err, "invalid JSON should be silently ignored")

	// Fractional numeric IDs are invalid JSON-RPC request IDs and must be ignored.
	req4 := &jsonrpc2.Request{
		Method: string(protocol.MethodCancelRequest),
		Params: jsontext.Value(`{"id":42.5}`),
	}
	result, err = p.Preempt(context.Background(), req4)
	assert.Nil(t, result)
	require.NoError(t, err, "fractional id should be silently ignored")

	// Numeric IDs outside int64 range must be ignored.
	req5 := &jsonrpc2.Request{
		Method: string(protocol.MethodCancelRequest),
		Params: jsontext.Value(`{"id":9223372036854775808}`),
	}
	result, err = p.Preempt(context.Background(), req5)
	assert.Nil(t, result)
	require.NoError(t, err, "out-of-range id should be silently ignored")
}

func TestCancelPreempter_TracksQueuedCalls(t *testing.T) {
	t.Parallel()

	s := New()
	p := &cancelPreempter{server: s}

	req := &jsonrpc2.Request{
		ID:     jsonrpc2.Int64ID(42),
		Method: string(protocol.MethodTextDocumentDiagnostic),
		Params: jsontext.Value(`{}`),
	}
	result, err := p.Preempt(context.Background(), req)
	assert.Nil(t, result)
	require.ErrorIs(t, err, jsonrpc2.ErrNotHandled)

	s.requestCancelMu.Lock()
	_, queued := s.requestQueuedIDs["i:42"]
	s.requestCancelMu.Unlock()
	assert.True(t, queued)
}

func TestCancelPreempter_PassesThroughOtherMethods(t *testing.T) {
	t.Parallel()

	p := &cancelPreempter{server: New()}

	req := &jsonrpc2.Request{
		Method: string(protocol.MethodTextDocumentDidOpen),
		Params: jsontext.Value(`{}`),
	}
	result, err := p.Preempt(context.Background(), req)
	assert.Nil(t, result)
	require.ErrorIs(t, err, jsonrpc2.ErrNotHandled)
}

func TestStartRequestContext_CancelsQueuedRequest(t *testing.T) {
	t.Parallel()

	s := New()
	id := jsonrpc2.Int64ID(7)
	s.noteQueuedRequest(id)
	s.cancelQueuedOrActiveRequest(id)

	ctx, done := s.startRequestContext(context.Background(), id)
	defer done()

	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestCancelQueuedOrActiveRequest_CancelsActiveContext(t *testing.T) {
	t.Parallel()

	s := New()
	id := jsonrpc2.StringID("req-1")

	ctx, done := s.startRequestContext(context.Background(), id)
	defer done()

	s.cancelQueuedOrActiveRequest(id)

	require.Eventually(t, func() bool {
		return ctx.Err() == context.Canceled
	}, time.Second, 10*time.Millisecond)
}

func TestLSPRequestError_MapsCanceledContext(t *testing.T) {
	t.Parallel()

	err := lspRequestError(context.Canceled)
	require.EqualError(t, err, "request cancelled")
}

func TestLSPRequestError_MapsDeadlineExceededToRequestFailed(t *testing.T) {
	t.Parallel()

	resp, err := jsonrpc2.NewResponse(jsonrpc2.Int64ID(1), nil, lspRequestError(context.DeadlineExceeded))
	require.NoError(t, err)

	wire, err := jsonrpc2.EncodeMessage(resp)
	require.NoError(t, err)

	var payload struct {
		Error struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, jsonv2.Unmarshal(wire, &payload))
	assert.Equal(t, int64(protocol.ErrorCodeRequestFailed), payload.Error.Code)
	assert.Equal(t, "request timed out", payload.Error.Message)
}

func TestParseCancelRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  jsontext.Value
		want jsonrpc2.ID
		ok   bool
	}{
		{name: "string", raw: jsontext.Value(`"req-1"`), want: jsonrpc2.StringID("req-1"), ok: true},
		{name: "integer", raw: jsontext.Value(`42`), want: jsonrpc2.Int64ID(42), ok: true},
		{name: "negative integer", raw: jsontext.Value(`-7`), want: jsonrpc2.Int64ID(-7), ok: true},
		{name: "fractional", raw: jsontext.Value(`42.5`), ok: false},
		{name: "scientific notation", raw: jsontext.Value(`1e3`), ok: false},
		{name: "too large", raw: jsontext.Value(`9223372036854775808`), ok: false},
		{name: "bool", raw: jsontext.Value(`true`), ok: false},
		{name: "null", raw: jsontext.Value(`null`), ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseCancelRequestID(tt.raw)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
