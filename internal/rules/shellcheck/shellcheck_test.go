package shellcheck

import (
	"testing"
	"time"
)

func TestShellcheckRunContextHasDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := shellcheckRunContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected shellcheck run context to have a deadline")
	}

	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > shellcheckRunTimeout {
		t.Fatalf("unexpected remaining timeout: %s", remaining)
	}
}
