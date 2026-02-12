package registry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tinovyatkin/tally/internal/async"
)

// mockImageResolver implements ImageResolver for testing.
type mockImageResolver struct {
	fn func(ctx context.Context, ref, platform string) (ImageConfig, error)
}

func (r *mockImageResolver) ResolveConfig(ctx context.Context, ref, platform string) (ImageConfig, error) {
	return r.fn(ctx, ref, platform)
}

func TestAsyncImageResolver_ID(t *testing.T) {
	t.Parallel()
	r := NewAsyncImageResolver(&mockImageResolver{})
	if r.ID() != registryResolverID {
		t.Errorf("expected ID %q, got %q", registryResolverID, r.ID())
	}
}

func TestAsyncImageResolver_Success(t *testing.T) {
	t.Parallel()
	inner := &mockImageResolver{
		fn: func(_ context.Context, ref, platform string) (ImageConfig, error) {
			return ImageConfig{
				Env:  map[string]string{"PATH": "/usr/bin"},
				OS:   "linux",
				Arch: "amd64",
			}, nil
		},
	}
	r := NewAsyncImageResolver(inner)

	result, err := r.Resolve(context.Background(), &ResolveRequest{Ref: "alpine:3.19", Platform: "linux/amd64"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, ok := result.(*ImageConfig)
	if !ok {
		t.Fatalf("expected *ImageConfig, got %T", result)
	}
	if cfg.OS != "linux" || cfg.Arch != "amd64" {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if cfg.Env["PATH"] != "/usr/bin" {
		t.Errorf("expected PATH=/usr/bin, got %q", cfg.Env["PATH"])
	}
}

func TestAsyncImageResolver_InvalidDataType(t *testing.T) {
	t.Parallel()
	r := NewAsyncImageResolver(&mockImageResolver{})

	_, err := r.Resolve(context.Background(), "not a ResolveRequest")
	if err == nil {
		t.Fatal("expected error for invalid data type")
	}
}

func TestAsyncImageResolver_NotFoundError_NoRetry(t *testing.T) {
	t.Parallel()
	callCount := 0
	inner := &mockImageResolver{
		fn: func(_ context.Context, ref, _ string) (ImageConfig, error) {
			callCount++
			return ImageConfig{}, &NotFoundError{Ref: ref, Err: errors.New("not found")}
		},
	}
	r := NewAsyncImageResolver(inner)

	_, err := r.Resolve(context.Background(), &ResolveRequest{Ref: "nonexistent:latest", Platform: "linux/amd64"})
	if err == nil {
		t.Fatal("expected error")
	}

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (no retry), got %d", callCount)
	}
}

func TestAsyncImageResolver_AuthError_RetriesOnce(t *testing.T) {
	t.Parallel()
	callCount := 0
	inner := &mockImageResolver{
		fn: func(_ context.Context, _ string, _ string) (ImageConfig, error) {
			callCount++
			return ImageConfig{}, &AuthError{Err: errors.New("unauthorized")}
		},
	}
	r := NewAsyncImageResolver(inner)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := r.Resolve(ctx, &ResolveRequest{Ref: "private:latest", Platform: "linux/amd64"})
	if err == nil {
		t.Fatal("expected error")
	}

	var authErr *AuthError
	if !errors.As(err, &authErr) {
		t.Errorf("expected AuthError, got %T: %v", err, err)
	}
	// Should retry once: 1 original + 1 retry = 2 calls.
	if callCount != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", callCount)
	}
}

func TestAsyncImageResolver_AuthError_SucceedsOnRetry(t *testing.T) {
	t.Parallel()
	callCount := 0
	inner := &mockImageResolver{
		fn: func(_ context.Context, _ string, _ string) (ImageConfig, error) {
			callCount++
			if callCount == 1 {
				return ImageConfig{}, &AuthError{Err: errors.New("unauthorized")}
			}
			return ImageConfig{OS: "linux", Arch: "amd64"}, nil
		},
	}
	r := NewAsyncImageResolver(inner)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := r.Resolve(ctx, &ResolveRequest{Ref: "image:latest", Platform: "linux/amd64"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, ok := result.(*ImageConfig)
	if !ok || cfg.OS != "linux" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestAsyncImageResolver_PlatformMismatch_NoRetry_ReturnsMismatchError(t *testing.T) {
	t.Parallel()
	callCount := 0
	inner := &mockImageResolver{
		fn: func(_ context.Context, ref, _ string) (ImageConfig, error) {
			callCount++
			return ImageConfig{OS: "linux", Arch: "arm64"}, &PlatformMismatchError{
				Ref:       ref,
				Requested: "linux/amd64",
				Available: []string{"linux/arm64"},
			}
		},
	}
	r := NewAsyncImageResolver(inner)

	// PlatformMismatchError is returned as the resolved value (not an error)
	// so handlers can access Available platforms for violation messages.
	result, err := r.Resolve(context.Background(), &ResolveRequest{Ref: "image:latest", Platform: "linux/amd64"})
	if err != nil {
		t.Fatalf("expected nil error for platform mismatch (handler detects it), got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (no retry), got %d", callCount)
	}
	// Result should be the PlatformMismatchError itself with Available platforms.
	platErr, ok := result.(*PlatformMismatchError)
	if !ok {
		t.Fatalf("expected *PlatformMismatchError, got %T", result)
	}
	if len(platErr.Available) != 1 || platErr.Available[0] != "linux/arm64" {
		t.Errorf("expected available [linux/arm64], got %v", platErr.Available)
	}
}

func TestAsyncImageResolver_NetworkError_Retries(t *testing.T) {
	t.Parallel()
	callCount := 0
	inner := &mockImageResolver{
		fn: func(_ context.Context, _ string, _ string) (ImageConfig, error) {
			callCount++
			if callCount <= 2 {
				return ImageConfig{}, &NetworkError{Err: errors.New("connection reset")}
			}
			return ImageConfig{OS: "linux", Arch: "amd64"}, nil
		},
	}
	r := NewAsyncImageResolver(inner)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := r.Resolve(ctx, &ResolveRequest{Ref: "image:latest", Platform: "linux/amd64"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, ok := result.(*ImageConfig)
	if !ok || cfg.OS != "linux" {
		t.Errorf("unexpected result: %v", result)
	}
	// 1 original + up to 2 retries. Since it succeeds on the 3rd call, we expect 3.
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestErrorTypes_SkipReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		reason async.SkipReason
	}{
		{
			name:   "AuthError",
			err:    &AuthError{Err: errors.New("unauthorized")},
			reason: async.SkipAuth,
		},
		{
			name:   "NetworkError",
			err:    &NetworkError{Err: errors.New("timeout")},
			reason: async.SkipNetwork,
		},
		{
			name:   "NotFoundError",
			err:    &NotFoundError{Ref: "foo", Err: errors.New("not found")},
			reason: async.SkipNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			type skipReasonable interface{ SkipReason() async.SkipReason }
			sr, ok := tc.err.(skipReasonable)
			if !ok {
				t.Fatalf("error does not implement SkipReason(): %T", tc.err)
			}
			if sr.SkipReason() != tc.reason {
				t.Errorf("expected %q, got %q", tc.reason, sr.SkipReason())
			}
		})
	}
}

func TestErrorTypes_ErrorMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "AuthError",
			err:     &AuthError{Err: errors.New("unauthorized")},
			wantMsg: "auth error: unauthorized",
		},
		{
			name:    "NetworkError",
			err:     &NetworkError{Err: errors.New("timeout")},
			wantMsg: "network error: timeout",
		},
		{
			name:    "NotFoundError",
			err:     &NotFoundError{Ref: "alpine:latest", Err: errors.New("manifest unknown")},
			wantMsg: "not found: alpine:latest: manifest unknown",
		},
		{
			name:    "PlatformMismatchError",
			err:     &PlatformMismatchError{Ref: "alpine:latest", Requested: "linux/amd64", Available: []string{"linux/arm64"}},
			wantMsg: "platform mismatch for alpine:latest: requested linux/amd64, available [linux/arm64]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.err.Error() != tc.wantMsg {
				t.Errorf("expected %q, got %q", tc.wantMsg, tc.err.Error())
			}
		})
	}
}

func TestErrorTypes_Unwrap(t *testing.T) {
	t.Parallel()
	inner := errors.New("original")

	tests := []struct {
		name string
		err  error
	}{
		{"AuthError", &AuthError{Err: inner}},
		{"NetworkError", &NetworkError{Err: inner}},
		{"NotFoundError", &NotFoundError{Ref: "x", Err: inner}},
		{"PlatformMismatchError", &PlatformMismatchError{Err: inner}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !errors.Is(tc.err, inner) {
				t.Error("expected errors.Is to find the wrapped error")
			}
		})
	}
}

func TestPlatformMismatchError_HasNoSkipReason(t *testing.T) {
	t.Parallel()
	// PlatformMismatchError should NOT implement SkipReason(), because it
	// becomes a violation, not a skip.
	err := &PlatformMismatchError{
		Ref:       "test",
		Requested: "linux/amd64",
		Available: []string{"linux/arm64"},
	}
	type skipReasonable interface{ SkipReason() async.SkipReason }
	if _, ok := any(err).(skipReasonable); ok {
		t.Error("PlatformMismatchError should NOT implement SkipReason()")
	}
}
