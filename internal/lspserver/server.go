// Package lspserver implements a Language Server Protocol server for tally.
//
// The server provides Dockerfile linting diagnostics, quick-fix code actions,
// and document formatting through the LSP protocol. It reuses the same lint
// pipeline as the CLI (dockerfile.Parse, semantic model, rules, processors).
//
// Transport: stdio only (--stdio).
// Protocol: LSP 3.17 types via internal/lsp/protocol, JSON-RPC via sourcegraph/jsonrpc2.
package lspserver

import (
	"context"
	stdjson "encoding/json"
	"log"
	"os"
	"strconv"
	"sync"

	jsonv2 "encoding/json/v2"
	"github.com/sourcegraph/jsonrpc2"

	protocol "github.com/tinovyatkin/tally/internal/lsp/protocol"
	"github.com/tinovyatkin/tally/internal/version"
)

const serverName = "tally"

// Server is the tally LSP server.
type Server struct {
	documents *DocumentStore
	lintCache *lintResultCache

	settingsMu sync.RWMutex
	settings   clientSettings

	diagMu                     sync.RWMutex
	pushDiagnostics            bool
	supportsDiagnosticRefresh  bool
	supportsDiagnosticPullMode bool
}

// New creates a new LSP server.
func New() *Server {
	return &Server{
		documents: NewDocumentStore(),
		lintCache: newLintResultCache(),
		settings:  defaultClientSettings(),
		// Default to push diagnostics (publishDiagnostics). If the client supports
		// the LSP 3.17 pull model, we switch to pull to avoid duplicate diagnostics.
		pushDiagnostics: true,
	}
}

// RunStdio starts the LSP server on stdin/stdout.
// It blocks until the connection is closed or the context is cancelled.
func (s *Server) RunStdio(ctx context.Context) error {
	stream := jsonrpc2.NewBufferedStream(stdioReadWriteCloser{}, jsonrpc2.VSCodeObjectCodec{})
	conn := jsonrpc2.NewConn(ctx, stream, jsonrpc2.HandlerWithError(s.handle))
	select {
	case <-ctx.Done():
		return conn.Close()
	case <-conn.DisconnectNotify():
		return nil
	}
}

// handle dispatches incoming JSON-RPC messages to the appropriate handler.
func (s *Server) handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) (any, error) {
	switch req.Method {
	// Lifecycle
	case "initialize":
		return unmarshalAndCall(req, s.handleInitialize)
	case "initialized", "$/setTrace":
		return nil, nil //nolint:nilnil // LSP: notifications have no result
	case "shutdown":
		return nil, nil //nolint:nilnil // LSP: shutdown returns null
	case "exit":
		return nil, conn.Close()

	// Document sync
	case "textDocument/didOpen":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidOpenTextDocumentParams) {
			s.handleDidOpen(ctx, conn, p)
		})
	case "textDocument/didChange":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidChangeTextDocumentParams) {
			s.handleDidChange(ctx, conn, p)
		})
	case "textDocument/didSave":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidSaveTextDocumentParams) {
			s.handleDidSave(ctx, conn, p)
		})
	case "textDocument/didClose":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidCloseTextDocumentParams) {
			s.handleDidClose(ctx, conn, p)
		})

	// Language features
	case "textDocument/codeAction":
		return unmarshalAndCall(req, s.handleCodeAction)
	case string(protocol.MethodTextDocumentDiagnostic):
		return unmarshalAndCall(req, s.handleDiagnostic)
	case string(protocol.MethodTextDocumentFormatting):
		return unmarshalAndCall(req, s.handleFormatting)

	// Workspace
	case "workspace/didChangeConfiguration":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidChangeConfigurationParams) {
			s.handleDidChangeConfiguration(ctx, conn, p)
		})
	case string(protocol.MethodWorkspaceExecuteCommand):
		return unmarshalAndCall(req, s.handleExecuteCommand)

	default:
		return nil, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeMethodNotFound,
			Message: "method not supported: " + req.Method,
		}
	}
}

// unmarshalAndCall unmarshals request params into T using json/v2
// and calls fn. The result is pre-marshaled with json/v2 so that
// union types with MarshalJSONTo serialize correctly through the stdlib-based
// jsonrpc2 transport.
func unmarshalAndCall[T any](req *jsonrpc2.Request, fn func(*T) (any, error)) (any, error) {
	var params T
	if req.Params != nil {
		if err := jsonv2.Unmarshal([]byte(*req.Params), &params); err != nil {
			return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
		}
	}
	result, err := fn(&params)
	if err != nil || result == nil {
		return result, err
	}
	// Pre-marshal with json/v2 so union types serialize correctly.
	raw, merr := jsonv2.Marshal(result)
	if merr != nil {
		return nil, merr
	}
	return stdjson.RawMessage(raw), nil
}

// unmarshalAndNotify unmarshals request params into T using json/v2
// and calls fn (for notifications that have no return).
func unmarshalAndNotify[T any](req *jsonrpc2.Request, fn func(*T)) error {
	var params T
	if req.Params != nil {
		if err := jsonv2.Unmarshal([]byte(*req.Params), &params); err != nil {
			return &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams, Message: err.Error()}
		}
	}
	fn(&params)
	return nil
}

// lspNotify pre-marshals params with json/v2 and sends via conn.Notify.
func lspNotify(ctx context.Context, conn *jsonrpc2.Conn, method string, params any) error {
	raw, err := jsonv2.Marshal(params)
	if err != nil {
		return err
	}
	return conn.Notify(ctx, method, stdjson.RawMessage(raw))
}

// handleInitialize responds to the initialize request with server capabilities.
func (s *Server) handleInitialize(params *protocol.InitializeParams) (any, error) {
	log.Printf("lsp: initialize from %s", clientInfoString(params))

	s.configureDiagnosticsMode(params)

	ver := version.RawVersion()

	return &protocol.InitializeResult{
		Capabilities: &protocol.ServerCapabilities{
			TextDocumentSync: &protocol.TextDocumentSyncOptionsOrKind{
				Options: &protocol.TextDocumentSyncOptions{
					OpenClose: ptrTo(true),
					Change:    ptrTo(protocol.TextDocumentSyncKindFull),
					Save: &protocol.BooleanOrSaveOptions{
						SaveOptions: &protocol.SaveOptions{IncludeText: ptrTo(true)},
					},
				},
			},
			CodeActionProvider: &protocol.BooleanOrCodeActionOptions{
				CodeActionOptions: &protocol.CodeActionOptions{
					CodeActionKinds: ptrTo([]protocol.CodeActionKind{
						protocol.CodeActionKindQuickFix,
						"source.fixAll.tally",
					}),
				},
			},
			DocumentFormattingProvider: &protocol.BooleanOrDocumentFormattingOptions{
				Boolean: ptrTo(true),
			},
			DiagnosticProvider: &protocol.DiagnosticOptionsOrRegistrationOptions{
				Options: &protocol.DiagnosticOptions{
					Identifier: ptrTo("tally"),
				},
			},
			ExecuteCommandProvider: &protocol.ExecuteCommandOptions{
				Commands: []string{
					applyAllFixesCommand,
				},
			},
		},
		ServerInfo: &protocol.ServerInfo{
			Name:    serverName,
			Version: &ver,
		},
	}, nil
}

// handleDidOpen lints the opened document and publishes diagnostics.
func (s *Server) handleDidOpen(ctx context.Context, conn *jsonrpc2.Conn, params *protocol.DidOpenTextDocumentParams) {
	if params.TextDocument == nil {
		return
	}
	uri := string(params.TextDocument.Uri)
	s.documents.Open(uri, string(params.TextDocument.LanguageId), params.TextDocument.Version, params.TextDocument.Text)

	if doc := s.documents.Get(uri); doc != nil && s.pushDiagnosticsEnabled() {
		s.publishDiagnostics(ctx, conn, doc)
	}
}

// handleDidChange updates the document and re-lints.
func (s *Server) handleDidChange(ctx context.Context, conn *jsonrpc2.Conn, params *protocol.DidChangeTextDocumentParams) {
	uri := string(params.TextDocument.Uri)

	// With full sync, there's exactly one content change containing the full text.
	for _, change := range params.ContentChanges {
		switch {
		case change.WholeDocument != nil:
			s.documents.Update(uri, params.TextDocument.Version, change.WholeDocument.Text)
		case change.Partial != nil:
			s.documents.Update(uri, params.TextDocument.Version, change.Partial.Text)
		}
	}

	if doc := s.documents.Get(uri); doc != nil && s.pushDiagnosticsEnabled() {
		s.publishDiagnostics(ctx, conn, doc)
	}
}

// handleDidSave re-lints on save.
func (s *Server) handleDidSave(ctx context.Context, conn *jsonrpc2.Conn, params *protocol.DidSaveTextDocumentParams) {
	uri := string(params.TextDocument.Uri)
	if params.Text != nil && *params.Text != "" {
		s.documents.Update(uri, 0, *params.Text)
	}

	if doc := s.documents.Get(uri); doc != nil && s.pushDiagnosticsEnabled() {
		s.publishDiagnostics(ctx, conn, doc)
	}
}

// handleDidClose clears diagnostics and removes the document.
func (s *Server) handleDidClose(ctx context.Context, conn *jsonrpc2.Conn, params *protocol.DidCloseTextDocumentParams) {
	uri := string(params.TextDocument.Uri)
	// Capture version before closing so clearDiagnostics can include it.
	var docVersion *int32
	if doc := s.documents.Get(uri); doc != nil {
		docVersion = &doc.Version
	}
	s.documents.Close(uri)
	s.lintCache.delete(uri)
	if s.pushDiagnosticsEnabled() {
		clearDiagnostics(ctx, conn, uri, docVersion)
	}
}

// handleCodeAction returns quick-fix code actions.
func (s *Server) handleCodeAction(params *protocol.CodeActionParams) (any, error) {
	doc := s.documents.Get(string(params.TextDocument.Uri))
	if doc == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no actions"
	}

	actions := s.codeActionsForDocument(doc, params)
	if len(actions) == 0 {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no actions"
	}
	return actions, nil
}

// clientInfoString formats client info for logging.
func clientInfoString(params *protocol.InitializeParams) string {
	if params == nil {
		return "unknown"
	}
	if params.ProcessId.Integer != nil {
		return "pid " + strconv.FormatInt(int64(*params.ProcessId.Integer), 10)
	}
	return "unknown"
}

func ptrTo[T any](v T) *T {
	return &v
}

// stdioReadWriteCloser wraps stdin/stdout as an io.ReadWriteCloser for JSON-RPC.
type stdioReadWriteCloser struct{}

func (stdioReadWriteCloser) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdioReadWriteCloser) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdioReadWriteCloser) Close() error                { return nil }
