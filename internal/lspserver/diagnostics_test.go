package lspserver

import (
	"context"
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
