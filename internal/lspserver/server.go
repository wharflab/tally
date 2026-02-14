// Package lspserver implements a Language Server Protocol server for tally.
//
// The server provides Dockerfile linting diagnostics, quick-fix code actions,
// and document formatting through the LSP protocol. It reuses the same lint
// pipeline as the CLI (dockerfile.Parse, semantic model, rules, processors).
//
// Transport: stdio only (--stdio).
// Protocol: LSP 3.17 types via internal/lsp/protocol, JSON-RPC via golang.org/x/exp/jsonrpc2.
package lspserver

import (
	"context"
	stdjson "encoding/json"
	"io"
	"log"
	"os"
	"strconv"
	"sync"

	jsonv2 "encoding/json/v2"
	"golang.org/x/exp/jsonrpc2"

	protocol "github.com/tinovyatkin/tally/internal/lsp/protocol"
	"github.com/tinovyatkin/tally/internal/version"
)

const serverName = "tally"

// jsonNull is an explicit JSON null value for call results.
// golang.org/x/exp/jsonrpc2 treats (nil, nil) as "no response" for calls,
// so we return this instead when the LSP result should be null.
var jsonNull = stdjson.RawMessage("null")

// Server is the tally LSP server.
type Server struct {
	conn   *jsonrpc2.Connection
	exitCh chan struct{} // closed when the "exit" notification is received

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
		exitCh:    make(chan struct{}),
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
	conn, err := jsonrpc2.Dial(ctx, stdioDialer{}, &serverBinder{server: s})
	if err != nil {
		return err
	}

	// Close connection when context is cancelled or the client sends "exit".
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-s.exitCh:
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	return conn.Wait()
}

// serverBinder binds a JSON-RPC connection to the server handler,
// capturing the connection reference for sending notifications.
type serverBinder struct {
	server *Server
}

func (b *serverBinder) Bind(_ context.Context, conn *jsonrpc2.Connection) (jsonrpc2.ConnectionOptions, error) {
	b.server.conn = conn
	return jsonrpc2.ConnectionOptions{
		Framer:  jsonrpc2.HeaderFramer(),
		Handler: jsonrpc2.HandlerFunc(b.server.handle),
	}, nil
}

// handle dispatches incoming JSON-RPC messages to the appropriate handler.
func (s *Server) handle(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	switch req.Method {
	// Lifecycle
	case "initialize":
		return unmarshalAndCall(req, s.handleInitialize)
	case "initialized", "$/setTrace":
		return nil, nil //nolint:nilnil // LSP: notifications have no result
	case "shutdown":
		return jsonNull, nil
	case "exit":
		select {
		case <-s.exitCh:
		default:
			close(s.exitCh)
		}
		return nil, nil //nolint:nilnil // LSP: exit is a notification

	// Document sync
	case "textDocument/didOpen":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidOpenTextDocumentParams) {
			s.handleDidOpen(ctx, p)
		})
	case "textDocument/didChange":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidChangeTextDocumentParams) {
			s.handleDidChange(ctx, p)
		})
	case "textDocument/didSave":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidSaveTextDocumentParams) {
			s.handleDidSave(ctx, p)
		})
	case "textDocument/didClose":
		return nil, unmarshalAndNotify(req, func(p *protocol.DidCloseTextDocumentParams) {
			s.handleDidClose(ctx, p)
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
			s.handleDidChangeConfiguration(ctx, p)
		})
	case string(protocol.MethodWorkspaceExecuteCommand):
		return unmarshalAndCall(req, s.handleExecuteCommand)

	default:
		return nil, jsonrpc2.NewError(int64(protocol.ErrorCodeMethodNotFound), "method not supported: "+req.Method)
	}
}

// unmarshalAndCall unmarshals request params into T using json/v2
// and calls fn. The result is pre-marshaled with json/v2 so that
// union types with MarshalJSONTo serialize correctly through the stdlib-based
// jsonrpc2 transport.
func unmarshalAndCall[T any](req *jsonrpc2.Request, fn func(*T) (any, error)) (any, error) {
	var params T
	if len(req.Params) > 0 {
		if err := jsonv2.Unmarshal(req.Params, &params); err != nil {
			return nil, jsonrpc2.NewError(int64(protocol.ErrorCodeInvalidParams), err.Error())
		}
	}
	result, err := fn(&params)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return jsonNull, nil
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
	if len(req.Params) > 0 {
		if err := jsonv2.Unmarshal(req.Params, &params); err != nil {
			return jsonrpc2.NewError(int64(protocol.ErrorCodeInvalidParams), err.Error())
		}
	}
	fn(&params)
	return nil
}

// lspNotify pre-marshals params with json/v2 and sends via conn.Notify.
func lspNotify(ctx context.Context, conn *jsonrpc2.Connection, method string, params any) error {
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
					OpenClose: new(true),
					Change:    ptrTo(protocol.TextDocumentSyncKindFull),
					Save: &protocol.BooleanOrSaveOptions{
						SaveOptions: &protocol.SaveOptions{IncludeText: new(true)},
					},
				},
			},
			CodeActionProvider: &protocol.BooleanOrCodeActionOptions{
				CodeActionOptions: &protocol.CodeActionOptions{
					CodeActionKinds: new([]protocol.CodeActionKind{
						protocol.CodeActionKindQuickFix,
						"source.fixAll.tally",
					}),
				},
			},
			DocumentFormattingProvider: &protocol.BooleanOrDocumentFormattingOptions{
				Boolean: new(true),
			},
			DiagnosticProvider: &protocol.DiagnosticOptionsOrRegistrationOptions{
				Options: &protocol.DiagnosticOptions{
					Identifier: new("tally"),
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
func (s *Server) handleDidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) {
	if params.TextDocument == nil {
		return
	}
	uri := string(params.TextDocument.Uri)
	s.documents.Open(uri, string(params.TextDocument.LanguageId), params.TextDocument.Version, params.TextDocument.Text)

	if doc := s.documents.Get(uri); doc != nil && s.pushDiagnosticsEnabled() {
		s.publishDiagnostics(ctx, doc)
	}
}

// handleDidChange updates the document and re-lints.
func (s *Server) handleDidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) {
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
		s.publishDiagnostics(ctx, doc)
	}
}

// handleDidSave re-lints on save.
func (s *Server) handleDidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) {
	uri := string(params.TextDocument.Uri)
	if params.Text != nil && *params.Text != "" {
		s.documents.Update(uri, 0, *params.Text)
	}

	if doc := s.documents.Get(uri); doc != nil && s.pushDiagnosticsEnabled() {
		s.publishDiagnostics(ctx, doc)
	}
}

// handleDidClose clears diagnostics and removes the document.
func (s *Server) handleDidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) {
	uri := string(params.TextDocument.Uri)
	// Capture version before closing so clearDiagnostics can include it.
	var docVersion *int32
	if doc := s.documents.Get(uri); doc != nil {
		docVersion = &doc.Version
	}
	s.documents.Close(uri)
	s.lintCache.delete(uri)
	if s.pushDiagnosticsEnabled() {
		clearDiagnostics(ctx, s.conn, uri, docVersion)
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
	return new(v)
}

// stdioDialer implements jsonrpc2.Dialer for stdin/stdout communication.
// It uses an io.Pipe intermediary so that Close reliably interrupts a blocked
// read on all platforms (closing os.Stdin from another goroutine does not
// unblock a concurrent read on macOS).
type stdioDialer struct{}

func (stdioDialer) Dial(_ context.Context) (io.ReadWriteCloser, error) {
	pr, pw := io.Pipe()
	go io.Copy(pw, os.Stdin) //nolint:errcheck // exits when pipe or stdin closes
	return &stdioRWC{pr: pr, pw: pw}, nil
}

// stdioRWC reads from an io.Pipe (fed by os.Stdin) and writes to os.Stdout.
type stdioRWC struct {
	pr *io.PipeReader
	pw *io.PipeWriter
}

func (s *stdioRWC) Read(p []byte) (int, error)  { return s.pr.Read(p) }
func (s *stdioRWC) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (s *stdioRWC) Close() error {
	_ = s.pw.Close() // unblocks any pending pr.Read with io.EOF
	return s.pr.Close()
}
