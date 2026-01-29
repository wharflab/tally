// Package rules provides the core rule system for the Dockerfile linter.
package rules

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Severity represents the severity level of a rule violation.
//
//nolint:recvcheck // UnmarshalJSON requires pointer receiver per json.Unmarshaler interface
type Severity int

const (
	// SeverityError indicates a critical issue that should fail the build.
	SeverityError Severity = iota
	// SeverityWarning indicates a significant issue that may cause problems.
	SeverityWarning
	// SeverityInfo indicates a suggestion or best practice recommendation.
	SeverityInfo
	// SeverityStyle indicates a style/formatting preference.
	SeverityStyle

	// SeverityOff disables the rule completely.
	// Placed after other severities to avoid zero-value confusion.
	SeverityOff
)

// String returns the string representation of the severity.
func (s Severity) String() string {
	switch s {
	case SeverityOff:
		return "off"
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	case SeverityStyle:
		return "style"
	default:
		return "unknown"
	}
}

// MarshalJSON implements json.Marshaler.
func (s Severity) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON implements json.Unmarshaler.
// Pointer receiver required by json.Unmarshaler interface.
func (s *Severity) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	parsed, err := ParseSeverity(str)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// ParseSeverity parses a severity string into a Severity value.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(s) {
	case "off":
		return SeverityOff, nil
	case "error":
		return SeverityError, nil
	case "warning", "warn":
		return SeverityWarning, nil
	case "info":
		return SeverityInfo, nil
	case "style":
		return SeverityStyle, nil
	default:
		return SeverityError, fmt.Errorf("unknown severity: %q", s)
	}
}

// IsMoreSevereThan returns true if s is more severe than other.
func (s Severity) IsMoreSevereThan(other Severity) bool {
	return s < other // Lower value = more severe
}

// IsAtLeast returns true if s is at least as severe as threshold.
func (s Severity) IsAtLeast(threshold Severity) bool {
	return s <= threshold
}
