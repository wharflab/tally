//go:build !cgo

package powershell

import "github.com/wharflab/tally/internal/highlight/core"

// Tokenize returns nil when cgo-backed PowerShell parsing is unavailable.
func Tokenize(script string) []core.Token {
	return nil
}
