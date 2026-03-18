//go:build !cgo

package batch

import "github.com/wharflab/tally/internal/highlight/core"

// Tokenize returns nil when cgo-backed batch parsing is unavailable.
func Tokenize(script string) []core.Token {
	return nil
}
