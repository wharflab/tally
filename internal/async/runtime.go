package async

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Runtime executes async check requests with concurrency limiting and timeouts.
type Runtime struct {
	// Concurrency is the max number of concurrent resolver calls. Default 4.
	Concurrency int

	// Timeout is the global wall-clock budget for the async session.
	Timeout time.Duration

	// Resolvers provides resolver lookup for this runtime instance.
	// When non-nil, used instead of the global resolver registry.
	// This allows isolated resolver sets per invocation (useful for testing
	// and for running multiple check sessions concurrently).
	Resolvers map[string]Resolver
}

// dedupeKey identifies a unique resolution unit.
type dedupeKey struct {
	resolverID string
	key        string
}

// resolveResult stores a cached resolution outcome.
type resolveResult struct {
	value any
	err   error
}

// Run executes the given requests under budget control and returns results.
func (rt *Runtime) Run(ctx context.Context, requests []CheckRequest) *RunResult {
	if len(requests) == 0 {
		return &RunResult{}
	}

	concurrency := rt.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}

	// Apply global timeout.
	if rt.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, rt.Timeout)
		defer cancel()
	}

	// Deduplicate requests by (ResolverID, Key).
	type pendingGroup struct {
		request  CheckRequest // representative request (for resolver call)
		handlers []ResultHandler
		requests []CheckRequest // all original requests (for skip reporting)
	}
	groups := make(map[dedupeKey]*pendingGroup)
	var orderedKeys []dedupeKey

	for _, req := range requests {
		dk := dedupeKey{resolverID: req.ResolverID, key: req.Key}
		if g, ok := groups[dk]; ok {
			g.handlers = append(g.handlers, req.Handler)
			g.requests = append(g.requests, req)
		} else {
			groups[dk] = &pendingGroup{
				request:  req,
				handlers: []ResultHandler{req.Handler},
				requests: []CheckRequest{req},
			}
			orderedKeys = append(orderedKeys, dk)
		}
	}

	// In-run cache: stores resolution results keyed by dedupeKey.
	cache := make(map[dedupeKey]*resolveResult)
	var cacheMu sync.Mutex

	// Collect results.
	var (
		allViolations []any
		allSkipped    []Skipped
		allCompleted  []CompletedCheck
		resultMu      sync.Mutex
	)

	// Semaphore channel for concurrency limiting.
	sem := make(chan struct{}, concurrency)

	var wg sync.WaitGroup
	for _, dk := range orderedKeys {
		group := groups[dk]

		wg.Add(1)
		go func(dk dedupeKey, group *pendingGroup) {
			defer wg.Done()

			// Acquire semaphore (respects context cancellation).
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				resultMu.Lock()
				for _, req := range group.requests {
					allSkipped = append(allSkipped, Skipped{
						Request: req,
						Reason:  SkipTimeout,
						Err:     ctx.Err(),
					})
				}
				resultMu.Unlock()
				return
			}

			// Check cache first.
			cacheMu.Lock()
			cached, hasCached := cache[dk]
			cacheMu.Unlock()

			var result *resolveResult
			if hasCached {
				result = cached
			} else {
				result = rt.resolve(ctx, group.request)
				cacheMu.Lock()
				cache[dk] = result
				cacheMu.Unlock()
			}

			// Process result.
			resultMu.Lock()
			defer resultMu.Unlock()

			if result.err != nil {
				reason := classifyError(result.err)
				for _, req := range group.requests {
					allSkipped = append(allSkipped, Skipped{
						Request: req,
						Reason:  reason,
						Err:     result.err,
					})
				}
				return
			}

			// Fan out resolved result to all handlers sharing this key.
			for i, handler := range group.handlers {
				violations := handler.OnSuccess(result.value)
				allViolations = append(allViolations, violations...)
				req := group.requests[i]
				allCompleted = append(allCompleted, CompletedCheck{
					RuleCode:   req.RuleCode,
					File:       req.File,
					StageIndex: req.StageIndex,
				})
			}
		}(dk, group)
	}

	wg.Wait()

	return &RunResult{
		Violations: allViolations,
		Skipped:    allSkipped,
		Completed:  allCompleted,
	}
}

// getResolver looks up a resolver by ID, checking the local map first,
// then falling back to the global registry.
func (rt *Runtime) getResolver(id string) Resolver {
	if rt.Resolvers != nil {
		return rt.Resolvers[id]
	}
	return GetResolver(id)
}

// resolve performs a single resolution with per-request timeout.
func (rt *Runtime) resolve(ctx context.Context, req CheckRequest) *resolveResult {
	resolver := rt.getResolver(req.ResolverID)
	if resolver == nil {
		return &resolveResult{err: errors.New("async: unknown resolver: " + req.ResolverID)}
	}

	// Apply per-request timeout (bounded by global deadline).
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	value, err := resolver.Resolve(ctx, req.Data)
	return &resolveResult{value: value, err: err}
}

// classifyError maps resolver errors to skip reasons.
func classifyError(err error) SkipReason {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return SkipTimeout
	}

	// Check for typed errors from the registry package.
	var skipErr interface{ SkipReason() SkipReason }
	if errors.As(err, &skipErr) {
		return skipErr.SkipReason()
	}

	return SkipResolverErr
}
