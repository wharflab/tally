package lspserver

import (
	"log"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"
)

func (s *Server) configureDiagnosticsMode(params *protocol.InitializeParams) {
	supportsPull := false
	supportsRefresh := false

	if params != nil && params.Capabilities != nil {
		if td := params.Capabilities.TextDocument; td != nil && td.Diagnostic != nil {
			supportsPull = true
		}
		if ws := params.Capabilities.Workspace; ws != nil && ws.Diagnostics != nil &&
			ws.Diagnostics.RefreshSupport != nil && *ws.Diagnostics.RefreshSupport {
			supportsRefresh = true
		}
	}

	// Default: if the client supports pull diagnostics (LSP 3.17), prefer pull and
	// disable publishDiagnostics to avoid duplicate diagnostics in editors like VS Code.
	push := true
	if params != nil && params.InitializationOptions != nil && params.InitializationOptions.DisablePushDiagnostics != nil {
		push = !*params.InitializationOptions.DisablePushDiagnostics
	} else if supportsPull {
		push = false
	}

	s.diagMu.Lock()
	s.pushDiagnostics = push
	s.supportsDiagnosticPullMode = supportsPull
	s.supportsDiagnosticRefresh = supportsRefresh
	s.diagMu.Unlock()

	if push {
		log.Printf("lsp: diagnostics mode: push (publishDiagnostics)")
		return
	}
	log.Printf("lsp: diagnostics mode: pull (textDocument/diagnostic), refreshSupport=%v", supportsRefresh)
}

func (s *Server) pushDiagnosticsEnabled() bool {
	s.diagMu.RLock()
	defer s.diagMu.RUnlock()
	return s.pushDiagnostics
}

func (s *Server) diagnosticRefreshSupported() bool {
	s.diagMu.RLock()
	defer s.diagMu.RUnlock()
	return s.supportsDiagnosticPullMode && s.supportsDiagnosticRefresh
}
