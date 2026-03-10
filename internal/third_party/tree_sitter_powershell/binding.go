//go:build cgo

//nolint:gocritic // cgo bindings require the special import "C" form in this vendored file.
package treesitterpowershell

// Vendored from github.com/airbus-cert/tree-sitter-powershell v0.24.4.
//
// We keep the full vendored grammar locally so builds stay deterministic and do
// not depend on the upstream Go binding's cgo include layout.
//
// Tracking issues for removing this vendored copy once upstream is consumable:
//   - https://github.com/airbus-cert/tree-sitter-powershell/issues/42
//   - https://github.com/tree-sitter/tree-sitter/issues/5421

// #cgo CFLAGS: -std=c11 -fPIC -I${SRCDIR}/src
// #include "src/parser.c"
// #include "src/scanner.c"
import "C"

import "unsafe"

// Language returns the tree-sitter language for the vendored PowerShell grammar.
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_powershell())
}
