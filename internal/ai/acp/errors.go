package acp

import (
	"errors"
	"fmt"
	"strings"
)

// ErrOutputLimitExceeded is used as a cancellation cause when the agent streams
// more output than we are willing to accept.
var ErrOutputLimitExceeded = errors.New("agent output limit exceeded")

// RunnerError wraps failures from running an ACP agent process.
//
// It intentionally includes a tail of the agent's stderr to aid diagnostics without
// corrupting structured outputs (JSON/SARIF) by streaming agent stderr directly.
type RunnerError struct {
	Op       string
	Err      error
	ExitCode *int
	Stderr   string
}

func (e *RunnerError) Error() string {
	if e == nil {
		return "<nil>"
	}
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	if e.Err != nil {
		b.WriteString(e.Err.Error())
	} else {
		b.WriteString("unknown error")
	}
	if e.ExitCode != nil {
		fmt.Fprintf(&b, " (exit=%d)", *e.ExitCode)
	}
	if s := strings.TrimSpace(e.Stderr); s != "" {
		b.WriteString("; agent stderr (tail): ")
		b.WriteString(s)
	}
	return b.String()
}

func (e *RunnerError) Unwrap() error { return e.Err }
