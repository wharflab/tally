package rules

import (
	"encoding/json"
	"testing"
)

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityError, "error"},
		{SeverityWarning, "warning"},
		{SeverityInfo, "info"},
		{SeverityStyle, "style"},
		{Severity(99), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.s.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSeverity_MarshalJSON(t *testing.T) {
	tests := []struct {
		s    Severity
		want string
	}{
		{SeverityError, `"error"`},
		{SeverityWarning, `"warning"`},
		{SeverityInfo, `"info"`},
		{SeverityStyle, `"style"`},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			data, err := json.Marshal(tc.s)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("Marshal = %s, want %s", data, tc.want)
			}
		})
	}
}

func TestSeverity_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input   string
		want    Severity
		wantErr bool
	}{
		{`"error"`, SeverityError, false},
		{`"warning"`, SeverityWarning, false},
		{`"warn"`, SeverityWarning, false},
		{`"info"`, SeverityInfo, false},
		{`"style"`, SeverityStyle, false},
		{`"ERROR"`, SeverityError, false}, // Case insensitive
		{`"unknown"`, SeverityError, true},
		{`123`, SeverityError, true}, // Not a string
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			var s Severity
			err := json.Unmarshal([]byte(tc.input), &s)
			if (err != nil) != tc.wantErr {
				t.Errorf("Unmarshal error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if !tc.wantErr && s != tc.want {
				t.Errorf("Unmarshal = %v, want %v", s, tc.want)
			}
		})
	}
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input   string
		want    Severity
		wantErr bool
	}{
		{"error", SeverityError, false},
		{"warning", SeverityWarning, false},
		{"warn", SeverityWarning, false},
		{"info", SeverityInfo, false},
		{"style", SeverityStyle, false},
		{"ERROR", SeverityError, false},
		{"invalid", SeverityError, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseSeverity(tc.input)
			if (err != nil) != tc.wantErr {
				t.Errorf("ParseSeverity error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("ParseSeverity = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSeverity_IsMoreSevereThan(t *testing.T) {
	tests := []struct {
		s, other Severity
		want     bool
	}{
		{SeverityError, SeverityWarning, true},
		{SeverityError, SeverityError, false},
		{SeverityWarning, SeverityError, false},
		{SeverityWarning, SeverityInfo, true},
		{SeverityStyle, SeverityError, false},
	}

	for _, tc := range tests {
		t.Run(tc.s.String()+"_vs_"+tc.other.String(), func(t *testing.T) {
			if got := tc.s.IsMoreSevereThan(tc.other); got != tc.want {
				t.Errorf("IsMoreSevereThan = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSeverity_IsAtLeast(t *testing.T) {
	tests := []struct {
		s, threshold Severity
		want         bool
	}{
		{SeverityError, SeverityError, true},
		{SeverityError, SeverityWarning, true},
		{SeverityWarning, SeverityError, false},
		{SeverityWarning, SeverityWarning, true},
		{SeverityInfo, SeverityWarning, false},
		{SeverityStyle, SeverityStyle, true},
	}

	for _, tc := range tests {
		t.Run(tc.s.String()+"_at_least_"+tc.threshold.String(), func(t *testing.T) {
			if got := tc.s.IsAtLeast(tc.threshold); got != tc.want {
				t.Errorf("IsAtLeast = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSeverityValues(t *testing.T) {
	// Verify enum values - SeverityOff is last to avoid zero-value confusion
	if SeverityError != 0 {
		t.Errorf("SeverityError should be 0 (zero value), got %d", SeverityError)
	}
	if SeverityOff < SeverityStyle {
		t.Errorf("SeverityOff should be last (highest value), got %d", SeverityOff)
	}

	// Zero value should be Error, not Off
	var v Violation
	if v.Severity != SeverityError {
		t.Errorf("Zero value should be SeverityError, got %v", v.Severity)
	}
}
