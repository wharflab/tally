package lspserver

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"
)

func TestPublishDiagnostics_UntitledURI(t *testing.T) {
	t.Parallel()

	s := New()
	uri := "untitled:Untitled-1"

	var receivedURI string
	var receivedContent []byte
	done := make(chan struct{})

	s.diagnosticsRunFn = func(_ context.Context, docURI string, _ int32, content []byte) {
		receivedURI = docURI
		receivedContent = append([]byte(nil), content...)
		close(done)
	}

	s.publishDiagnostics(context.Background(), &Document{
		URI:        uri,
		LanguageID: "dockerfile",
		Version:    1,
		Content:    "FROM alpine\nRUN apt-get update\n",
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for diagnostics")
	}

	assert.Equal(t, uri, receivedURI)
	assert.Equal(t, "FROM alpine\nRUN apt-get update\n", string(receivedContent))
}

func TestHandleDiagnostic_UntitledURI_ClosedDocument(t *testing.T) {
	t.Parallel()

	s := New()
	// Don't open the document — simulate a pull diagnostic request for a closed untitled doc.
	result, err := s.handleDiagnostic(context.Background(), &protocol.DocumentDiagnosticParams{
		TextDocument: protocol.TextDocumentIdentifier{
			Uri: "untitled:Untitled-1",
		},
	})
	require.NoError(t, err)

	resp, ok := result.(*protocol.DocumentDiagnosticResponse)
	require.True(t, ok)
	require.NotNil(t, resp.FullDocumentDiagnosticReport)
	assert.Empty(t, resp.FullDocumentDiagnosticReport.Items)
}

func TestHandleDiagnostic_CanceledContextForOpenDocument(t *testing.T) {
	t.Parallel()

	s := New()
	uri := "file:///tmp/Dockerfile"
	s.documents.Open(uri, "dockerfile", 1, "FROM alpine\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := s.handleDiagnostic(ctx, &protocol.DocumentDiagnosticParams{
		TextDocument: protocol.TextDocumentIdentifier{
			Uri: protocol.DocumentUri(uri),
		},
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, result)
}

func TestPublishDiagnostics_CoalescesPerURI(t *testing.T) {
	t.Parallel()

	s := New()
	uri := "file:///tmp/Dockerfile"

	var versionsMu sync.Mutex
	versions := make([]int32, 0, 2)
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	var runs atomic.Int32

	s.diagnosticsRunFn = func(_ context.Context, docURI string, version int32, _ []byte) {
		assert.Equal(t, uri, docURI)

		run := runs.Add(1)
		versionsMu.Lock()
		versions = append(versions, version)
		versionsMu.Unlock()

		switch run {
		case 1:
			close(firstStarted)
			<-firstRelease
		case 2:
			close(secondStarted)
			<-secondRelease
		default:
			t.Errorf("unexpected diagnostics run count: %d", run)
		}
	}

	s.publishDiagnostics(context.Background(), &Document{URI: uri, Version: 1, Content: "FROM alpine"})
	<-firstStarted

	s.publishDiagnostics(context.Background(), &Document{URI: uri, Version: 2, Content: "FROM busybox"})
	s.publishDiagnostics(context.Background(), &Document{URI: uri, Version: 3, Content: "FROM debian"})

	close(firstRelease)
	<-secondStarted
	close(secondRelease)

	require.Eventually(t, func() bool {
		return runs.Load() == 2
	}, time.Second, 10*time.Millisecond)

	versionsMu.Lock()
	got := append([]int32(nil), versions...)
	versionsMu.Unlock()
	assert.Equal(t, []int32{1, 3}, got)
}

func TestPublishDiagnostics_BoundedConcurrency(t *testing.T) {
	t.Parallel()

	s := New()
	s.diagnosticsConcurrencyGate = make(chan struct{}, 1)

	release := make(chan struct{})
	var current atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup
	wg.Add(3)

	s.diagnosticsRunFn = func(_ context.Context, _ string, _ int32, _ []byte) {
		cur := current.Add(1)
		for {
			currentMax := maxConcurrent.Load()
			if cur <= currentMax || maxConcurrent.CompareAndSwap(currentMax, cur) {
				break
			}
		}

		<-release
		current.Add(-1)
		wg.Done()
	}

	s.publishDiagnostics(context.Background(), &Document{URI: "file:///tmp/one.Dockerfile", Version: 1, Content: "FROM alpine"})
	s.publishDiagnostics(context.Background(), &Document{URI: "file:///tmp/two.Dockerfile", Version: 1, Content: "FROM alpine"})
	s.publishDiagnostics(context.Background(), &Document{URI: "file:///tmp/three.Dockerfile", Version: 1, Content: "FROM alpine"})

	require.Eventually(t, func() bool {
		return maxConcurrent.Load() == 1
	}, time.Second, 10*time.Millisecond)

	close(release)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for diagnostics workers")
	}

	assert.Equal(t, int32(1), maxConcurrent.Load())
}
