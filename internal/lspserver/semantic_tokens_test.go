package lspserver

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wharflab/tally/internal/highlight"
	protocol "github.com/wharflab/tally/internal/lsp/protocol"
)

var analyzeSemanticDocumentTestMu sync.Mutex

func TestHandleSemanticTokens_CanceledContextSkipsAnalysis(t *testing.T) {
	t.Parallel()
	analyzeSemanticDocumentTestMu.Lock()
	t.Cleanup(analyzeSemanticDocumentTestMu.Unlock)

	s := New()
	uri := "file:///tmp/Dockerfile"
	s.documents.Open(uri, "dockerfile", 1, "FROM alpine\nRUN echo hello\n")

	original := analyzeSemanticDocument
	t.Cleanup(func() { analyzeSemanticDocument = original })

	var calls atomic.Int32
	analyzeSemanticDocument = func(file string, content []byte) *highlight.Document {
		calls.Add(1)
		return original(file, content)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := s.handleSemanticTokensFull(ctx, &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{Uri: protocol.DocumentUri(uri)},
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
	assert.Zero(t, calls.Load())
}

func TestHandleSemanticTokens_ReusesCachedAnalysisAcrossHandlers(t *testing.T) {
	t.Parallel()
	analyzeSemanticDocumentTestMu.Lock()
	t.Cleanup(analyzeSemanticDocumentTestMu.Unlock)

	s := New()
	uri := "file:///tmp/Dockerfile"
	s.documents.Open(uri, "dockerfile", 1, "FROM alpine\nRUN echo \"$HOME\"\n")

	original := analyzeSemanticDocument
	t.Cleanup(func() { analyzeSemanticDocument = original })

	var calls atomic.Int32
	analyzeSemanticDocument = func(file string, content []byte) *highlight.Document {
		calls.Add(1)
		return original(file, content)
	}

	ctx := context.Background()

	fullResult, err := s.handleSemanticTokensFull(ctx, &protocol.SemanticTokensParams{
		TextDocument: protocol.TextDocumentIdentifier{Uri: protocol.DocumentUri(uri)},
	})
	require.NoError(t, err)

	fullResp, ok := fullResult.(*protocol.SemanticTokensOrNull)
	require.True(t, ok)
	require.NotNil(t, fullResp.SemanticTokens)
	require.NotEmpty(t, fullResp.SemanticTokens.Data)

	rangeResult, err := s.handleSemanticTokensRange(ctx, &protocol.SemanticTokensRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{Uri: protocol.DocumentUri(uri)},
		Range: protocol.Range{
			Start: protocol.Position{Line: 1, Character: 0},
			End:   protocol.Position{Line: 1, Character: 100},
		},
	})
	require.NoError(t, err)

	rangeResp, ok := rangeResult.(*protocol.SemanticTokensOrNull)
	require.True(t, ok)
	require.NotNil(t, rangeResp.SemanticTokens)
	require.NotEmpty(t, rangeResp.SemanticTokens.Data)
	assert.Equal(t, int32(1), calls.Load())
}
