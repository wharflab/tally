package wasm

import _ "embed"

// Binary is the embedded ShellCheck WASI WebAssembly module.
//
//go:embed shellcheck.wasm
var Binary []byte
