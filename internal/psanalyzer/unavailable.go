package psanalyzer

import "errors"

// ErrUnavailable marks local PowerShell analyzer startup as unavailable.
var ErrUnavailable = errors.New("PowerShell analyzer unavailable")

type unavailableError struct {
	err error
}

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

// IsUnavailable reports whether err means the local PowerShell analyzer runtime
// is not available and callers should skip PowerShell analysis or formatting.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}
