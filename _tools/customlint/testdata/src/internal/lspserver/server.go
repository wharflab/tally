package lspserver

// Simulated constants (stand-ins for protocol.Method*).
// In the real code these are imported from internal/lsp/protocol.
var methodInitialize, methodShutdown string

func dispatch(method string) {
	switch method {
	// Bad: hardcoded LSP method literals.
	case "initialize": // want `use protocol.Method\* constant instead of string literal "initialize" for LSP method name`
		return
	case "shutdown": // want `use protocol.Method\* constant instead of string literal "shutdown" for LSP method name`
		return
	case "exit": // want `use protocol.Method\* constant instead of string literal "exit" for LSP method name`
		return
	case "textDocument/didOpen": // want `use protocol.Method\* constant instead of string literal "textDocument/didOpen" for LSP method name`
		return
	case "textDocument/codeAction": // want `use protocol.Method\* constant instead of string literal "textDocument/codeAction" for LSP method name`
		return
	case "workspace/didChangeConfiguration": // want `use protocol.Method\* constant instead of string literal "workspace/didChangeConfiguration" for LSP method name`
		return
	case "$/cancelRequest": // want `use protocol.Method\* constant instead of string literal "\$/cancelRequest" for LSP method name`
		return

	// Good: using constants — not flagged.
	case methodInitialize:
		return
	case methodShutdown:
		return

	// Good: unrelated string literals — not flagged.
	case "something/else":
		return
	}

	// Good: non-LSP strings in other contexts.
	_ = "hello world"

	// Bad: LSP literal used outside switch.
	_ = "textDocument/didSave" // want `use protocol.Method\* constant instead of string literal "textDocument/didSave" for LSP method name`
}
