package config

import "github.com/gkampitakis/ciinfo"

const slowChecksModeOff = "off"

// SlowChecksEnabled returns whether slow checks should run based on the mode.
// "on" → true, "off" → false, "auto" → enabled when not running in CI.
func SlowChecksEnabled(mode string) bool {
	switch mode {
	case "on":
		return true
	case slowChecksModeOff:
		return false
	default: // "auto"
		return !ciinfo.IsCI
	}
}

// CIName returns the detected CI provider name, or empty string if not in CI.
func CIName() string {
	if !ciinfo.IsCI {
		return ""
	}
	return ciinfo.Name
}
