//go:build !cgo

package treesitterpowershell

import "unsafe"

// Language returns nil when cgo is unavailable so callers can fall back.
func Language() unsafe.Pointer {
	return nil
}
