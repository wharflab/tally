package wasm

import _ "embed"

// Binary is the embedded ShellCheck WASI WebAssembly module.
//
// The wasm artifact is not checked in; build it with `make shellcheck-wasm`
// (requires Docker) or download it from a recent CI run. See
// _tools/shellcheck-wasm/ for the build recipe.
//
//go:embed shellcheck.wasm
var Binary []byte
