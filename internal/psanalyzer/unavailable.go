package psanalyzer

import (
	"errors"
	"sync"
)

// ErrUnavailable marks local PowerShell analyzer startup as unavailable.
var ErrUnavailable = errors.New("PowerShell analyzer unavailable")

type unavailableError struct {
	err error
}

// UnavailableEvent describes a skipped PowerShell analyzer operation.
type UnavailableEvent struct {
	Operation string
	Err       error
}

// UnavailableReporter receives non-diagnostic analyzer availability notices.
type UnavailableReporter func(UnavailableEvent)

var unavailableReporter = struct {
	sync.Mutex

	fn UnavailableReporter
}{}

func (e unavailableError) Error() string {
	if e.err == nil {
		return ErrUnavailable.Error()
	}
	return e.err.Error()
}

func (e unavailableError) Unwrap() []error {
	if e.err == nil {
		return []error{ErrUnavailable}
	}
	return []error{ErrUnavailable, e.err}
}

func newUnavailableError(err error) error {
	if err == nil {
		return ErrUnavailable
	}
	return unavailableError{err: err}
}

// SetUnavailableReporter installs a process-local reporter for expected local
// PowerShell analyzer availability skips. The returned function restores the
// previous reporter.
func SetUnavailableReporter(reporter UnavailableReporter) func() {
	unavailableReporter.Lock()
	prev := unavailableReporter.fn
	unavailableReporter.fn = reporter
	unavailableReporter.Unlock()

	return func() {
		unavailableReporter.Lock()
		unavailableReporter.fn = prev
		unavailableReporter.Unlock()
	}
}

// IsUnavailable reports whether err means the local PowerShell analyzer runtime
// is not available and callers should skip PowerShell analysis or formatting.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

func reportUnavailable(operation string, err error) {
	unavailableReporter.Lock()
	reporter := unavailableReporter.fn
	unavailableReporter.Unlock()

	if reporter != nil {
		reporter(UnavailableEvent{Operation: operation, Err: err})
	}
}
