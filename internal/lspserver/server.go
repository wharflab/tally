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
	"encoding/json/jsontext"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	jsonv2 "encoding/json/v2"
	"golang.org/x/exp/jsonrpc2"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"
	"github.com/wharflab/tally/internal/version"
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
	semCache  *semanticDocCache

	diagnosticsDispatchMu      sync.Mutex
	diagnosticsInFlightByURI   map[string]bool
	diagnosticsPendingByURI    map[string]diagnosticsTask
	diagnosticsConcurrencyGate chan struct{}
	diagnosticsRunFn           func(ctx context.Context, docURI string, version int32, content []byte)

	settingsMu sync.RWMutex
	settings   clientSettings

	watchedFilesMu    sync.Mutex
	watchedFilesTimer *time.Timer
	watchedFilesSeq   uint64

	diagMu                     sync.RWMutex
	pushDiagnostics            bool
	supportsDiagnosticRefresh  bool
	supportsDiagnosticPullMode bool
	showDocumentSupported      bool

	requestCancelMu          sync.Mutex
	requestQueuedIDs         map[string]struct{}
	requestCanceledQueuedIDs map[string]struct{}
	activeRequestCancelsByID map[string]context.CancelFunc
}

// New creates a new LSP server.
func New() *Server {
	return &Server{
		exitCh:                   make(chan struct{}),
		documents:                NewDocumentStore(),
		lintCache:                newLintResultCache(),
		semCache:                 newSemanticDocCache(),
		diagnosticsInFlightByURI: make(map[string]bool),
		diagnosticsPendingByURI:  make(map[string]diagnosticsTask),
		diagnosticsConcurrencyGate: make(
			chan struct{},
			maxConcurrentDiagnosticsPasses,
		),
		settings: defaultClientSettings(),
		// Default to push diagnostics (publishDiagnostics). If the client supports
		// the LSP 3.17 pull model, we switch to pull to avoid duplicate diagnostics.
		pushDiagnostics:          true,
		requestQueuedIDs:         make(map[string]struct{}),
		requestCanceledQueuedIDs: make(map[string]struct{}),
		activeRequestCancelsByID: make(map[string]context.CancelFunc),
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
		Framer:    jsonrpc2.HeaderFramer(),
		Preempter: &cancelPreempter{server: b.server},
		Handler:   jsonrpc2.HandlerFunc(b.server.handle),
	}, nil
}

// cancelPreempter handles $/cancelRequest notifications before they enter the
// message queue. This prevents "method not supported" errors when the client
// sends cancellation notifications.
type cancelPreempter struct {
	server *Server
}

// cancelRequestParams is the LSP CancelParams sent with $/cancelRequest.
type cancelRequestParams struct {
	ID jsontext.Value `json:"id"`
}

func (p *cancelPreempter) Preempt(_ context.Context, req *jsonrpc2.Request) (any, error) {
	if req.Method != string(protocol.MethodCancelRequest) {
		if req.IsCall() {
			p.server.noteQueuedRequest(req.ID)
		}
		return nil, jsonrpc2.ErrNotHandled
	}

	var params cancelRequestParams
	if err := jsonv2.Unmarshal(req.Params, &params); err != nil {
		return nil, nil //nolint:nilerr,nilnil // malformed cancel — intentionally ignored
	}

	id, _ := parseCancelRequestID(params.ID)
	if id.IsValid() {
		p.server.cancelQueuedOrActiveRequest(id)
	}
	return nil, nil //nolint:nilnil // notification — no response needed
}

func parseCancelRequestID(raw jsontext.Value) (jsonrpc2.ID, bool) {
	token := strings.TrimSpace(string(raw))
	if token == "" || token == "null" {
		return jsonrpc2.ID{}, false
	}

	if token[0] == '"' {
		var id string
		if err := jsonv2.Unmarshal(raw, &id); err != nil {
			return jsonrpc2.ID{}, false
		}
		return jsonrpc2.StringID(id), true
	}

	n, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return jsonrpc2.ID{}, false
	}
	return jsonrpc2.Int64ID(n), true
}

// handle dispatches incoming JSON-RPC messages to the appropriate handler.
func (s *Server) handle(ctx context.Context, req *jsonrpc2.Request) (any, error) {
	if req.IsCall() {
		var done func()
		ctx, done = s.startRequestContext(ctx, req.ID)
		defer done()

		if err := ctx.Err(); err != nil {
			return nil, lspRequestError(err)
		}
	}

	switch req.Method {
	// Lifecycle
	case string(protocol.MethodInitialize):
		return unmarshalAndCall(req, s.handleInitialize)
	case string(protocol.MethodInitialized),
		string(protocol.MethodSetTrace),
		string(protocol.MethodCancelRequest),
		string(protocol.MethodProgress):
		return nil, nil //nolint:nilnil // LSP: notifications have no result
	case string(protocol.MethodShutdown):
		return jsonNull, nil
	case string(protocol.MethodExit):
		select {
		case <-s.exitCh:
		default:
			close(s.exitCh)
		}
		return nil, nil //nolint:nilnil // LSP: exit is a notification

	// Document sync
	case string(protocol.MethodTextDocumentDidOpen):
		return nil, unmarshalAndNotify(req, func(p *protocol.DidOpenTextDocumentParams) {
			s.handleDidOpen(ctx, p)
		})
	case string(protocol.MethodTextDocumentDidChange):
		return nil, unmarshalAndNotify(req, func(p *protocol.DidChangeTextDocumentParams) {
			s.handleDidChange(ctx, p)
		})
	case string(protocol.MethodTextDocumentDidSave):
		return nil, unmarshalAndNotify(req, func(p *protocol.DidSaveTextDocumentParams) {
			s.handleDidSave(ctx, p)
		})
	case string(protocol.MethodTextDocumentDidClose):
		return nil, unmarshalAndNotify(req, func(p *protocol.DidCloseTextDocumentParams) {
			s.handleDidClose(ctx, p)
		})

	// Language features
	case string(protocol.MethodTextDocumentCodeAction):
		return unmarshalAndCall(req, func(p *protocol.CodeActionParams) (any, error) {
			return s.handleCodeAction(ctx, p)
		})
	case string(protocol.MethodTextDocumentDiagnostic):
		return unmarshalAndCall(req, func(p *protocol.DocumentDiagnosticParams) (any, error) {
			return s.handleDiagnostic(ctx, p)
		})
	case string(protocol.MethodTextDocumentFormatting):
		return unmarshalAndCall(req, func(p *protocol.DocumentFormattingParams) (any, error) {
			return s.handleFormatting(ctx, p)
		})
	case string(protocol.MethodTextDocumentSemanticTokensFull):
		return unmarshalAndCall(req, func(p *protocol.SemanticTokensParams) (any, error) {
			return s.handleSemanticTokensFull(ctx, p)
		})
	case string(protocol.MethodTextDocumentSemanticTokensRange):
		return unmarshalAndCall(req, func(p *protocol.SemanticTokensRangeParams) (any, error) {
			return s.handleSemanticTokensRange(ctx, p)
		})

	// Workspace
	case string(protocol.MethodWorkspaceDidChangeConfiguration):
		return nil, unmarshalAndNotify(req, func(p *protocol.DidChangeConfigurationParams) {
			s.handleDidChangeConfiguration(ctx, p)
		})
	case string(protocol.MethodWorkspaceDidChangeWatchedFiles):
		return nil, unmarshalAndNotify(req, func(p *protocol.DidChangeWatchedFilesParams) {
			s.handleDidChangeWatchedFiles(ctx, p)
		})
	case string(protocol.MethodWorkspaceExecuteCommand):
		return unmarshalAndCall(req, func(p *protocol.ExecuteCommandParams) (any, error) {
			return s.handleExecuteCommand(ctx, p)
		})

	default:
		return nil, jsonrpc2.NewError(int64(protocol.ErrorCodeMethodNotFound), "method not supported: "+req.Method)
	}
}

func requestIDKey(id jsonrpc2.ID) string {
	if !id.IsValid() {
		return ""
	}

	switch v := id.Raw().(type) {
	case int64:
		return "i:" + strconv.FormatInt(v, 10)
	case string:
		return "s:" + v
	default:
		return ""
	}
}

func (s *Server) noteQueuedRequest(id jsonrpc2.ID) {
	key := requestIDKey(id)
	if key == "" {
		return
	}

	s.requestCancelMu.Lock()
	s.requestQueuedIDs[key] = struct{}{}
	s.requestCancelMu.Unlock()
}

func (s *Server) cancelQueuedOrActiveRequest(id jsonrpc2.ID) {
	key := requestIDKey(id)
	if key == "" {
		return
	}

	s.requestCancelMu.Lock()
	cancel := s.activeRequestCancelsByID[key]
	if cancel == nil {
		if _, queued := s.requestQueuedIDs[key]; queued {
			s.requestCanceledQueuedIDs[key] = struct{}{}
		}
		s.requestCancelMu.Unlock()
		return
	}
	s.requestCancelMu.Unlock()

	cancel()
}

func (s *Server) startRequestContext(parent context.Context, id jsonrpc2.ID) (context.Context, func()) {
	key := requestIDKey(id)
	if key == "" {
		return parent, func() {}
	}

	ctx, cancel := context.WithCancel(parent)

	s.requestCancelMu.Lock()
	delete(s.requestQueuedIDs, key)
	if _, canceled := s.requestCanceledQueuedIDs[key]; canceled {
		delete(s.requestCanceledQueuedIDs, key)
		cancel()
	}
	s.activeRequestCancelsByID[key] = cancel
	s.requestCancelMu.Unlock()

	return ctx, func() {
		s.requestCancelMu.Lock()
		delete(s.requestQueuedIDs, key)
		delete(s.requestCanceledQueuedIDs, key)
		delete(s.activeRequestCancelsByID, key)
		cancel()
		s.requestCancelMu.Unlock()
	}
}

func lspRequestError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return jsonrpc2.NewError(int64(protocol.ErrorCodeRequestCancelled), "request cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return jsonrpc2.NewError(int64(protocol.ErrorCodeRequestFailed), "request timed out")
	default:
		return err
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
		return nil, lspRequestError(err)
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

// lspCall pre-marshals params with json/v2 and sends a call via conn.Call.
// The response is awaited but the result is discarded.
func lspCall(ctx context.Context, conn *jsonrpc2.Connection, method string, params any) error {
	raw, err := jsonv2.Marshal(params)
	if err != nil {
		return err
	}
	return conn.Call(ctx, method, stdjson.RawMessage(raw)).Await(ctx, nil)
}

// handleInitialize responds to the initialize request with server capabilities.
func (s *Server) handleInitialize(params *protocol.InitializeParams) (any, error) {
	log.Printf("lsp: initialize from %s", clientInfoString(params))

	s.configureDiagnosticsMode(params)
	if params.Capabilities != nil &&
		params.Capabilities.Window != nil &&
		params.Capabilities.Window.ShowDocument != nil {
		s.showDocumentSupported = params.Capabilities.Window.ShowDocument.Support
	}
	if params.InitializationOptions != nil {
		if next, ok := parseClientSettings(*params.InitializationOptions); ok {
			s.settingsMu.Lock()
			s.settings = next
			s.settingsMu.Unlock()
		}
	}

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
						suppressCodeActionKind,
						"source.fixAll.tally",
					}),
				},
			},
			DocumentFormattingProvider: &protocol.BooleanOrDocumentFormattingOptions{
				Boolean: new(true),
			},
			SemanticTokensProvider: &protocol.SemanticTokensOptionsOrRegistrationOptions{
				Options: &protocol.SemanticTokensOptions{
					Legend: &protocol.SemanticTokensLegend{
						TokenTypes:     lspSemanticTokenTypes(),
						TokenModifiers: lspSemanticTokenModifiers(),
					},
					Range: &protocol.BooleanOrEmptyObject{Boolean: new(true)},
					Full:  &protocol.BooleanOrSemanticTokensFullDelta{Boolean: new(true)},
				},
			},
			DiagnosticProvider: &protocol.DiagnosticOptionsOrRegistrationOptions{
				Options: &protocol.DiagnosticOptions{
					Identifier: new("tally"),
				},
			},
			ExecuteCommandProvider: &protocol.ExecuteCommandOptions{
				Commands: []string{
					applyAllFixesCommand,
					openRuleDocCommand,
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
	s.cancelPendingDiagnostics(uri)
	s.documents.Close(uri)
	s.lintCache.delete(uri)
	s.semCache.delete(uri)
	if s.pushDiagnosticsEnabled() {
		clearDiagnostics(ctx, s.conn, uri, docVersion)
	}
}

// handleCodeAction returns quick-fix code actions.
func (s *Server) handleCodeAction(ctx context.Context, params *protocol.CodeActionParams) (any, error) {
	doc := s.documents.Get(string(params.TextDocument.Uri))
	if doc == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no actions"
	}

	actions := s.codeActionsForDocument(ctx, doc, params)
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
	go func() {
		_, err := io.Copy(pw, os.Stdin)
		// Treat stdin closure as clean EOF, not an error.
		// When the LSP client stops, it closes stdin, which should trigger
		// a graceful shutdown without logging errors.
		if err == nil || errors.Is(err, io.EOF) || errors.Is(err, fs.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
			_ = pw.Close() // Clean close triggers EOF on read side
		} else {
			_ = pw.CloseWithError(err) // Propagate actual I/O errors
		}
	}()
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
