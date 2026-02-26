package shellcheck

import (
	"context"
	"testing"
)

func TestRuntimeInitContext_DetachesCancellation(t *testing.T) {
	t.Parallel()

	type keyType string
	const key keyType = "k"

	parent, cancel := context.WithCancel(context.WithValue(context.Background(), key, "v")) //nolint:revive // test-only context value
	initCtx := runtimeInitContext(parent)
	cancel()

	if err := initCtx.Err(); err != nil {
		t.Fatalf("expected init context without cancellation, got err=%v", err)
	}
	if got := initCtx.Value(key); got != "v" {
		t.Fatalf("init context value=%v, want v", got)
	}
}

func TestRuntimeInitContext_NilContext(t *testing.T) {
	t.Parallel()

	initCtx := runtimeInitContext(nil)
	if initCtx == nil {
		t.Fatal("expected non-nil init context")
	}
	if err := initCtx.Err(); err != nil {
		t.Fatalf("unexpected init context err: %v", err)
	}
}
