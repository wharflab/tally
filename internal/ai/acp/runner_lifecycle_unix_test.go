//go:build !windows

package acp

import (
	"context"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunner_TerminatesOnTimeoutAndCleansProcessGroup(t *testing.T) {
	t.Parallel()

	r := NewRunner(WithTerminateGrace(50 * time.Millisecond))

	_, err := r.Run(context.Background(), RunRequest{
		// Intentionally not an ACP agent: we want an init-time timeout plus a
		// deterministic background child process to validate process-group cleanup.
		Command: []string{"sh", "-c", "sleep 10000 & echo TEST_CHILD_PID=$! 1>&2; sleep 10000"},
		Cwd:     t.TempDir(),
		Timeout: 500 * time.Millisecond,
		Prompt:  "hang please",
	})
	if err == nil {
		t.Fatalf("Run() expected error, got nil")
	}
	pid := mustParseChildPID(t, err.Error())
	waitProcessGone(t, pid, 5*time.Second)
}

func TestRunner_TerminatesOnEarlyAgentErrorAndCleansProcessGroup(t *testing.T) {
	t.Parallel()

	r := NewRunner(WithTerminateGrace(50 * time.Millisecond))

	_, err := r.Run(context.Background(), RunRequest{
		Command: []string{testAgentBin, "-mode=error-newsession", "-spawn-child=true"},
		Cwd:     t.TempDir(),
		Timeout: 5 * time.Second,
		Prompt:  "oops",
	})
	if err == nil {
		t.Fatalf("Run() expected error, got nil")
	}
	pid := mustParseChildPID(t, err.Error())
	waitProcessGone(t, pid, 5*time.Second)
}

func TestRunner_TerminatesOnMalformedOutputAndCleansProcessGroup(t *testing.T) {
	t.Parallel()

	r := NewRunner(WithTerminateGrace(50 * time.Millisecond))

	_, err := r.Run(context.Background(), RunRequest{
		Command: []string{testAgentBin, "-mode=malformed", "-spawn-child=true"},
		Cwd:     t.TempDir(),
		Timeout: 5 * time.Second,
		Prompt:  "ignored",
	})
	if err == nil {
		t.Fatalf("Run() expected error, got nil")
	}
	pid := mustParseChildPID(t, err.Error())
	waitProcessGone(t, pid, 5*time.Second)
}

func mustParseChildPID(t *testing.T, msg string) int {
	t.Helper()
	const key = "TEST_CHILD_PID="
	i := strings.LastIndex(msg, key)
	if i < 0 {
		t.Fatalf("missing %q in error: %q", key, msg)
	}
	s := msg[i+len(key):]
	s = strings.TrimSpace(s)
	// PID is at the beginning of the line.
	end := strings.IndexByte(s, '\n')
	if end >= 0 {
		s = s[:end]
	}
	pid, err := strconv.Atoi(s)
	if err != nil || pid <= 0 {
		t.Fatalf("invalid child pid %q in error: %v", s, err)
	}
	return pid
}

func waitProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process %d still exists after %v", pid, timeout)
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means "exists but we don't have permission".
	return err == syscall.EPERM
}
