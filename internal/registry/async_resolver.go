package registry

import (
	"context"
	"errors"
	"fmt"
	"time"

	backoff "github.com/cenkalti/backoff/v5"
)

// AsyncImageResolver adapts an ImageResolver to the async.Resolver interface
// with retry logic per the error contract.
type AsyncImageResolver struct {
	inner ImageResolver
}

// NewAsyncImageResolver creates a new async resolver adapter.
func NewAsyncImageResolver(inner ImageResolver) *AsyncImageResolver {
	return &AsyncImageResolver{inner: inner}
}

// ID returns the resolver identifier.
func (r *AsyncImageResolver) ID() string { return registryResolverID }

// Resolve executes the image resolution with retry logic.
//
// Retry policy per error type:
//   - PlatformMismatchError: no retry, returns partial config + error (becomes a violation)
//   - NotFoundError: no retry (permanent)
//   - AuthError: retry once after backoff
//   - NetworkError / other: retry with exponential backoff (up to 3 total attempts)
func (r *AsyncImageResolver) Resolve(ctx context.Context, data any) (any, error) {
	req, ok := data.(*ResolveRequest)
	if !ok {
		return nil, fmt.Errorf("registry resolver: unexpected data type %T", data)
	}

	cfg, err := r.resolveWithRetry(ctx, req.Ref, req.Platform)
	if err != nil {
		// PlatformMismatchError is NOT a skip — return the error itself as the
		// resolved value so handlers can access Available platforms for the
		// violation message (rather than a zero-value ImageConfig).
		var platErr *PlatformMismatchError
		if errors.As(err, &platErr) {
			return platErr, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *AsyncImageResolver) resolveWithRetry(ctx context.Context, ref, platform string) (ImageConfig, error) {
	var authRetried bool

	return backoff.Retry(ctx, func() (ImageConfig, error) {
		cfg, err := r.inner.ResolveConfig(ctx, ref, platform)
		if err == nil {
			return cfg, nil
		}

		// PlatformMismatchError: not a skip, return partial config for rule to handle.
		var platErr *PlatformMismatchError
		if errors.As(err, &platErr) {
			return cfg, backoff.Permanent(err)
		}

		// NotFoundError: permanent, no retry.
		var notFound *NotFoundError
		if errors.As(err, &notFound) {
			return ImageConfig{}, backoff.Permanent(err)
		}

		// AuthError: retry once, then give up.
		var authErr *AuthError
		if errors.As(err, &authErr) {
			if authRetried {
				return ImageConfig{}, backoff.Permanent(err)
			}
			authRetried = true
			return ImageConfig{}, err
		}

		// NetworkError / other: retryable with backoff.
		return ImageConfig{}, err
	},
		backoff.WithBackOff(newResolverBackoff()),
		backoff.WithMaxTries(3),       // 1 original + 2 retries
		backoff.WithMaxElapsedTime(0), // rely on context for overall timeout
	)
}

func newResolverBackoff() *backoff.ExponentialBackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 500 * time.Millisecond
	b.MaxInterval = 5 * time.Second
	b.Multiplier = 2.0
	return b
}
