package async

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockResolver implements Resolver for testing.
type mockResolver struct {
	id        string
	fn        func(ctx context.Context, data any) (any, error)
	callCount atomic.Int32
}

func (r *mockResolver) ID() string { return r.id }

func (r *mockResolver) Resolve(ctx context.Context, data any) (any, error) {
	r.callCount.Add(1)
	return r.fn(ctx, data)
}

// mockHandler implements ResultHandler for testing.
type mockHandler struct {
	onSuccess func(resolved any) []any
}

func (h *mockHandler) OnSuccess(resolved any) []any {
	if h.onSuccess != nil {
		return h.onSuccess(resolved)
	}
	return nil
}

// skipError implements the SkipReason() interface for testing error classification.
type skipError struct {
	reason SkipReason
	msg    string
}

func (e *skipError) Error() string          { return e.msg }
func (e *skipError) SkipReason() SkipReason { return e.reason }

// newTestRuntime creates a Runtime with the given resolver injected locally.
func newTestRuntime(r *mockResolver, concurrency int, timeout time.Duration) *Runtime {
	return &Runtime{
		Concurrency: concurrency,
		Timeout:     timeout,
		Resolvers:   map[string]Resolver{r.ID(): r},
	}
}

func TestRuntime_EmptyRequests(t *testing.T) {
	t.Parallel()
	rt := &Runtime{Concurrency: 4, Timeout: 5 * time.Second}
	result := rt.Run(context.Background(), nil)

	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
	if len(result.Resolved) != 0 {
		t.Errorf("expected 0 resolved, got %d", len(result.Resolved))
	}
}

func TestRuntime_SingleRequest(t *testing.T) {
	t.Parallel()
	resolver := &mockResolver{
		id: "test",
		fn: func(_ context.Context, data any) (any, error) {
			s, ok := data.(string)
			if !ok {
				return nil, errors.New("expected string data")
			}
			return "resolved:" + s, nil
		},
	}
	rt := newTestRuntime(resolver, 4, 5*time.Second)

	var handlerCalled bool
	requests := []CheckRequest{{
		RuleCode:   "test-rule",
		Category:   CategoryNetwork,
		Key:        "key1",
		ResolverID: "test",
		Data:       "input1",
		Handler: &mockHandler{
			onSuccess: func(resolved any) []any {
				handlerCalled = true
				if resolved != "resolved:input1" {
					t.Errorf("expected resolved:input1, got %v", resolved)
				}
				return []any{"violation1"}
			},
		},
	}}

	result := rt.Run(context.Background(), requests)

	if !handlerCalled {
		t.Error("handler was not called")
	}
	if len(result.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(result.Violations))
	}
	if result.Violations[0] != "violation1" {
		t.Errorf("expected violation1, got %v", result.Violations[0])
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved entry, got %d", len(result.Resolved))
	}
	if got, ok := result.Resolved[ResolutionKey{ResolverID: "test", Key: "key1"}]; !ok {
		t.Error("expected resolved entry for (test, key1)")
	} else if got != "resolved:input1" {
		t.Errorf("expected resolved:input1 in resolved map, got %v", got)
	}
}

func TestRuntime_Deduplication(t *testing.T) {
	t.Parallel()
	resolver := &mockResolver{
		id: "test",
		fn: func(_ context.Context, _ any) (any, error) {
			return "resolved", nil
		},
	}
	rt := newTestRuntime(resolver, 4, 5*time.Second)

	var handler1Called, handler2Called atomic.Bool
	requests := []CheckRequest{
		{
			RuleCode:   "rule-a",
			Key:        "same-key",
			ResolverID: "test",
			Data:       "data",
			Handler: &mockHandler{
				onSuccess: func(_ any) []any {
					handler1Called.Store(true)
					return []any{"v1"}
				},
			},
		},
		{
			RuleCode:   "rule-b",
			Key:        "same-key",
			ResolverID: "test",
			Data:       "data",
			Handler: &mockHandler{
				onSuccess: func(_ any) []any {
					handler2Called.Store(true)
					return []any{"v2"}
				},
			},
		},
	}

	result := rt.Run(context.Background(), requests)

	if !handler1Called.Load() || !handler2Called.Load() {
		t.Error("both handlers should have been called")
	}
	// Resolver should only be called once due to deduplication.
	if resolver.callCount.Load() != 1 {
		t.Errorf("expected resolver called once, got %d", resolver.callCount.Load())
	}
	if len(result.Violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(result.Violations))
	}
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved entry, got %d", len(result.Resolved))
	}
	if got, ok := result.Resolved[ResolutionKey{ResolverID: "test", Key: "same-key"}]; !ok {
		t.Error("expected resolved entry for (test, same-key)")
	} else if got != "resolved" {
		t.Errorf("expected resolved in resolved map, got %v", got)
	}
}

func TestRuntime_DifferentKeys(t *testing.T) {
	t.Parallel()
	resolver := &mockResolver{
		id: "test",
		fn: func(_ context.Context, _ any) (any, error) {
			return "resolved", nil
		},
	}
	rt := newTestRuntime(resolver, 4, 5*time.Second)

	requests := []CheckRequest{
		{
			RuleCode:   "rule-a",
			Key:        "key-1",
			ResolverID: "test",
			Data:       "data1",
			Handler:    &mockHandler{onSuccess: func(_ any) []any { return nil }},
		},
		{
			RuleCode:   "rule-b",
			Key:        "key-2",
			ResolverID: "test",
			Data:       "data2",
			Handler:    &mockHandler{onSuccess: func(_ any) []any { return nil }},
		},
	}

	rt.Run(context.Background(), requests)

	// Different keys should result in separate resolver calls.
	if resolver.callCount.Load() != 2 {
		t.Errorf("expected resolver called twice, got %d", resolver.callCount.Load())
	}
}

func TestRuntime_GlobalTimeout(t *testing.T) {
	t.Parallel()
	resolver := &mockResolver{
		id: "test",
		fn: func(ctx context.Context, _ any) (any, error) {
			// Block until context is cancelled.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	// Very short global timeout, concurrency=1 so one blocks and the other queues.
	rt := newTestRuntime(resolver, 1, 50*time.Millisecond)

	requests := []CheckRequest{
		{
			RuleCode:   "rule-slow",
			Key:        "key1",
			ResolverID: "test",
			Data:       "data",
			Handler:    &mockHandler{},
		},
		{
			RuleCode:   "rule-queued",
			Key:        "key2",
			ResolverID: "test",
			Data:       "data2",
			Handler:    &mockHandler{},
		},
	}

	result := rt.Run(context.Background(), requests)

	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
	// Both should be skipped due to timeout (one during execution, one waiting for semaphore).
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped, got %d", len(result.Skipped))
	}
	for _, s := range result.Skipped {
		if s.Reason != SkipTimeout {
			t.Errorf("expected skip reason %q, got %q", SkipTimeout, s.Reason)
		}
	}
}

func TestRuntime_PerRequestTimeout(t *testing.T) {
	t.Parallel()
	resolver := &mockResolver{
		id: "test",
		fn: func(ctx context.Context, _ any) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	// Long global timeout, short per-request timeout.
	rt := newTestRuntime(resolver, 4, 10*time.Second)

	requests := []CheckRequest{{
		RuleCode:   "rule",
		Key:        "key1",
		ResolverID: "test",
		Data:       "data",
		Timeout:    50 * time.Millisecond,
		Handler:    &mockHandler{},
	}}

	result := rt.Run(context.Background(), requests)

	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(result.Skipped))
	}
	if result.Skipped[0].Reason != SkipTimeout {
		t.Errorf("expected skip reason %q, got %q", SkipTimeout, result.Skipped[0].Reason)
	}
}

func TestRuntime_ResolverError_Classification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantReason SkipReason
	}{
		{
			name:       "context deadline exceeded",
			err:        context.DeadlineExceeded,
			wantReason: SkipTimeout,
		},
		{
			name:       "context canceled",
			err:        context.Canceled,
			wantReason: SkipTimeout,
		},
		{
			name:       "skip reason interface",
			err:        &skipError{reason: SkipAuth, msg: "auth failed"},
			wantReason: SkipAuth,
		},
		{
			name:       "not found via interface",
			err:        &skipError{reason: SkipNotFound, msg: "not found"},
			wantReason: SkipNotFound,
		},
		{
			name:       "generic error",
			err:        errors.New("something went wrong"),
			wantReason: SkipResolverErr,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyError(tc.err)
			if got != tc.wantReason {
				t.Errorf("classifyError(%v) = %q, want %q", tc.err, got, tc.wantReason)
			}
		})
	}
}

func TestRuntime_ResolverError_SkipsAllHandlers(t *testing.T) {
	t.Parallel()
	resolver := &mockResolver{
		id: "test",
		fn: func(_ context.Context, _ any) (any, error) {
			return nil, &skipError{reason: SkipNetwork, msg: "network error"}
		},
	}
	rt := newTestRuntime(resolver, 4, 5*time.Second)

	requests := []CheckRequest{
		{
			RuleCode:   "rule-a",
			Key:        "same-key",
			ResolverID: "test",
			Data:       "data",
			Handler:    &mockHandler{onSuccess: func(_ any) []any { t.Error("should not be called"); return nil }},
		},
		{
			RuleCode:   "rule-b",
			Key:        "same-key",
			ResolverID: "test",
			Data:       "data",
			Handler:    &mockHandler{onSuccess: func(_ any) []any { t.Error("should not be called"); return nil }},
		},
	}

	result := rt.Run(context.Background(), requests)

	if len(result.Violations) != 0 {
		t.Errorf("expected 0 violations, got %d", len(result.Violations))
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped, got %d", len(result.Skipped))
	}
	for _, s := range result.Skipped {
		if s.Reason != SkipNetwork {
			t.Errorf("expected skip reason %q, got %q", SkipNetwork, s.Reason)
		}
	}
}

func TestRuntime_UnknownResolver(t *testing.T) {
	t.Parallel()
	// Empty resolver map — no resolver registered for "nonexistent".
	rt := &Runtime{
		Concurrency: 4,
		Timeout:     5 * time.Second,
		Resolvers:   map[string]Resolver{},
	}

	requests := []CheckRequest{{
		RuleCode:   "rule",
		Key:        "key1",
		ResolverID: "nonexistent",
		Data:       "data",
		Handler:    &mockHandler{},
	}}

	result := rt.Run(context.Background(), requests)

	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(result.Skipped))
	}
	if result.Skipped[0].Reason != SkipResolverErr {
		t.Errorf("expected reason %q, got %q", SkipResolverErr, result.Skipped[0].Reason)
	}
}

func TestRuntime_ConcurrencyLimit(t *testing.T) {
	t.Parallel()

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	resolver := &mockResolver{
		id: "test",
		fn: func(_ context.Context, _ any) (any, error) {
			cur := concurrent.Add(1)
			// Track max concurrency.
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			concurrent.Add(-1)
			return "ok", nil
		},
	}

	const limit = 2
	rt := newTestRuntime(resolver, limit, 10*time.Second)

	// Create more requests than the concurrency limit.
	requests := make([]CheckRequest, 0, 6)
	for i := range 6 {
		requests = append(requests, CheckRequest{
			RuleCode:   "rule",
			Key:        string(rune('a' + i)),
			ResolverID: "test",
			Data:       "data",
			Handler:    &mockHandler{},
		})
	}

	rt.Run(context.Background(), requests)

	if maxConcurrent.Load() > int32(limit) {
		t.Errorf("max concurrent = %d, should not exceed %d", maxConcurrent.Load(), limit)
	}
}

func TestRuntime_DefaultConcurrency(t *testing.T) {
	t.Parallel()
	resolver := &mockResolver{
		id: "test",
		fn: func(_ context.Context, _ any) (any, error) {
			return "ok", nil
		},
	}
	// 0 concurrency should default to 4.
	rt := newTestRuntime(resolver, 0, 5*time.Second)

	requests := []CheckRequest{{
		RuleCode:   "rule",
		Key:        "key1",
		ResolverID: "test",
		Data:       "data",
		Handler:    &mockHandler{},
	}}

	result := rt.Run(context.Background(), requests)
	// Should complete without error.
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
}

func TestRuntime_FallsBackToGlobalRegistry(t *testing.T) {
	t.Parallel()
	resolver := &mockResolver{
		id: "global-test",
		fn: func(_ context.Context, _ any) (any, error) {
			return "from-global", nil
		},
	}
	RegisterResolver(resolver)
	t.Cleanup(func() {
		resolverMu.Lock()
		delete(resolvers, "global-test")
		resolverMu.Unlock()
	})

	// Runtime with nil Resolvers → falls back to global.
	rt := &Runtime{Concurrency: 4, Timeout: 5 * time.Second}

	var got any
	requests := []CheckRequest{{
		RuleCode:   "rule",
		Key:        "key1",
		ResolverID: "global-test",
		Data:       "data",
		Handler: &mockHandler{
			onSuccess: func(resolved any) []any {
				got = resolved
				return nil
			},
		},
	}}

	rt.Run(context.Background(), requests)

	if got != "from-global" {
		t.Errorf("expected from-global, got %v", got)
	}
}

func TestResolverRegistry(t *testing.T) {
	t.Parallel()

	// Use a unique ID to avoid interfering with other parallel tests.
	const id = "registry-test-unique"

	// Initially absent.
	if r := GetResolver(id); r != nil {
		t.Error("expected nil for unregistered resolver")
	}

	// Register and retrieve.
	resolver := &mockResolver{id: id}
	RegisterResolver(resolver)
	t.Cleanup(func() {
		resolverMu.Lock()
		delete(resolvers, id)
		resolverMu.Unlock()
	})

	if r := GetResolver(id); r == nil {
		t.Error("expected non-nil after registration")
	} else if r.ID() != id {
		t.Errorf("expected ID %q, got %q", id, r.ID())
	}
}
