package lspserver

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleFullPass_ReplacesExistingTimer(t *testing.T) {
	t.Parallel()

	s := New()
	uri := "file:///tmp/Dockerfile"

	s.scheduleFullPass(context.Background(), uri, 1, nil, nil, nil)

	s.shellcheckDebounceMu.Lock()
	first := s.shellcheckDebounce[uri]
	s.shellcheckDebounceMu.Unlock()
	require.NotNil(t, first)

	s.scheduleFullPass(context.Background(), uri, 2, nil, nil, nil)

	s.shellcheckDebounceMu.Lock()
	second := s.shellcheckDebounce[uri]
	s.shellcheckDebounceMu.Unlock()
	require.NotNil(t, second)
	assert.NotSame(t, first, second)

	s.cancelShellcheckDebounce(uri)
}

func TestScheduleFullPass_CleansUpTimerAfterCallback(t *testing.T) {
	t.Parallel()

	s := New()
	uri := "file:///tmp/Dockerfile"

	// Version does not exist in DocumentStore, so callback should return quickly
	// without running a full lint pass, while still cleaning up the timer entry.
	s.scheduleFullPass(context.Background(), uri, 1, nil, nil, nil)

	require.Eventually(t, func() bool {
		s.shellcheckDebounceMu.Lock()
		defer s.shellcheckDebounceMu.Unlock()
		return s.shellcheckDebounce[uri] == nil
	}, 2*time.Second, 25*time.Millisecond)
}

func TestCancelAllShellcheckDebounce_ClearsPendingTimers(t *testing.T) {
	t.Parallel()

	s := New()
	s.scheduleFullPass(context.Background(), "file:///tmp/one.Dockerfile", 1, nil, nil, nil)
	s.scheduleFullPass(context.Background(), "file:///tmp/two.Dockerfile", 1, nil, nil, nil)

	s.cancelAllShellcheckDebounce()

	s.shellcheckDebounceMu.Lock()
	defer s.shellcheckDebounceMu.Unlock()
	assert.Empty(t, s.shellcheckDebounce)
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
