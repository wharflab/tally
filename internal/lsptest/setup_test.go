package lsptest

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	tflsp "github.com/TypeFox/go-lsp/protocol"
	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/require"
)

var (
	binaryPath  string
	coverageDir string
)

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "tally-lsptest")
	if err != nil {
		panic(err)
	}

	binaryName := "tally"
	if runtime.GOOS == "windows" {
		binaryName = "tally.exe"
	}
	binaryPath = filepath.Join(tmpDir, binaryName)

	// Collect coverage only when GOCOVERDIR is set (Linux CI).
	buildArgs := []string{"build"}
	coverageDir = os.Getenv("GOCOVERDIR")
	if coverageDir != "" {
		coverageDir, err = filepath.Abs(coverageDir)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			panic("failed to get absolute coverage directory path: " + err.Error())
		}
		if err := os.MkdirAll(coverageDir, 0o750); err != nil {
			_ = os.RemoveAll(tmpDir)
			panic("failed to create coverage directory: " + err.Error())
		}
		buildArgs = append(buildArgs, "-cover")
	}
	buildArgs = append(buildArgs, "-o", binaryPath, "github.com/tinovyatkin/tally")

	cmd := exec.Command("go", buildArgs...)
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=jsonv2")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(tmpDir)
		panic("failed to build binary: " + string(out))
	}

	code := m.Run()
	_ = os.RemoveAll(tmpDir)
	os.Exit(code)
}

// processIO wraps subprocess stdin/stdout as an io.ReadWriteCloser
// for use with jsonrpc2.NewBufferedStream.
type processIO struct {
	reader io.ReadCloser
	writer io.WriteCloser
}

func (p *processIO) Read(data []byte) (int, error)  { return p.reader.Read(data) }
func (p *processIO) Write(data []byte) (int, error) { return p.writer.Write(data) }
func (p *processIO) Close() error {
	if err := p.reader.Close(); err != nil {
		_ = p.writer.Close()
		return err
	}
	return p.writer.Close()
}

// diagnosticsHandler routes server-to-client notifications and captures diagnostics.
type diagnosticsHandler struct {
	diagnosticsCh chan *publishDiagnosticsParams
}

func (h *diagnosticsHandler) Handle(_ context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	if req.Method == "textDocument/publishDiagnostics" && req.Params != nil {
		var params publishDiagnosticsParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			panic("diagnosticsHandler: unmarshal failed: " + err.Error())
		}
		h.diagnosticsCh <- &params
	}
	// Notifications don't need a reply.
}

// testServer manages a tally lsp --stdio subprocess for black-box testing.
type testServer struct {
	cmd    *exec.Cmd
	conn   *jsonrpc2.Conn
	stderr *bytes.Buffer

	diagnosticsCh chan *publishDiagnosticsParams

	waitOnce sync.Once
	waitErr  error
}

// wait calls cmd.Wait exactly once, returning the cached result on subsequent calls.
func (ts *testServer) wait() error {
	ts.waitOnce.Do(func() { ts.waitErr = ts.cmd.Wait() })
	return ts.waitErr
}

// startTestServer launches tally lsp --stdio as a subprocess with
// Content-Length-framed JSON-RPC over stdin/stdout.
func startTestServer(t *testing.T) *testServer {
	t.Helper()

	cmd := exec.Command(binaryPath, "lsp", "--stdio")
	cmd.Env = append(os.Environ(), "GOCOVERDIR="+coverageDir)

	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Start())

	ts := &testServer{
		cmd:           cmd,
		stderr:        &stderr,
		diagnosticsCh: make(chan *publishDiagnosticsParams, 10),
	}

	stream := jsonrpc2.NewBufferedStream(&processIO{reader: stdout, writer: stdin}, jsonrpc2.VSCodeObjectCodec{})
	conn := jsonrpc2.NewConn(context.Background(), stream, &diagnosticsHandler{diagnosticsCh: ts.diagnosticsCh})
	ts.conn = conn

	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Logf("lsp conn close: %v", err)
		}
		// Wait for process with timeout; kill if it doesn't exit.
		// Uses ts.wait() so cmd.Wait is only called once even if the
		// test already waited (e.g., TestLSP_ShutdownExit).
		done := make(chan error, 1)
		go func() { done <- ts.wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if err := cmd.Process.Kill(); err != nil {
				t.Logf("kill lsp server: %v", err)
			}
			<-done
		}
		// Log stderr after process exit to avoid data race on the buffer.
		if t.Failed() {
			t.Logf("server stderr:\n%s", stderr.String())
		}
	})

	return ts
}

// initialize sends initialize + initialized and returns the server capabilities.
func (ts *testServer) initialize(t *testing.T) initializeResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var result initializeResult
	err := ts.conn.Call(ctx, "initialize", &initializeParams{
		ProcessID:    nil,
		RootURI:      nil,
		Capabilities: jsontext.Value(`{}`),
		ClientInfo: &clientInfo{
			Name:    "tally-lsptest",
			Version: "1.0.0",
		},
	}, &result)
	require.NoError(t, err)

	require.NoError(t, ts.conn.Notify(ctx, "initialized", struct{}{}))

	return result
}

const diagTimeout = 10 * time.Second

// waitDiagnostics blocks until a publishDiagnostics notification arrives or timeout.
func (ts *testServer) waitDiagnostics(t *testing.T) *publishDiagnosticsParams {
	t.Helper()
	select {
	case d := <-ts.diagnosticsCh:
		return d
	case <-time.After(diagTimeout):
		t.Fatal("timed out waiting for diagnostics")
		return nil
	}
}

// shutdown sends the shutdown request followed by exit notification.
func (ts *testServer) shutdown(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ts.conn.Call(ctx, "shutdown", nil, nil)
	require.NoError(t, err)

	require.NoError(t, ts.conn.Notify(ctx, "exit", nil))
}

// openDocument sends textDocument/didOpen.
func (ts *testServer) openDocument(t *testing.T, uri, content string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ts.conn.Notify(ctx, "textDocument/didOpen", &didOpenTextDocumentParams{
		TextDocument: textDocumentItem{
			URI:        uri,
			LanguageID: "dockerfile",
			Version:    1,
			Text:       content,
		},
	}))
}

// changeDocument sends textDocument/didChange with full sync.
func (ts *testServer) changeDocument(t *testing.T, uri string, version int32, content string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ts.conn.Notify(ctx, "textDocument/didChange", &didChangeTextDocumentParams{
		TextDocument: versionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: []textDocumentContentChangeEvent{
			{Text: content},
		},
	}))
}

// closeDocument sends textDocument/didClose.
func (ts *testServer) closeDocument(t *testing.T, uri string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ts.conn.Notify(ctx, "textDocument/didClose", &didCloseTextDocumentParams{
		TextDocument: textDocumentIdentifier{URI: uri},
	}))
}

// saveDocument sends textDocument/didSave with text included.
func (ts *testServer) saveDocument(t *testing.T, uri, content string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ts.conn.Notify(ctx, "textDocument/didSave", &didSaveTextDocumentParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Text:         content,
	}))
}

// LSP types for the test client.
// We define minimal types here rather than importing go.lsp.dev/protocol,
// using field types that match the JSON wire format.

type initializeParams struct {
	ProcessID    *int           `json:"processId"`
	RootURI      *string        `json:"rootUri"`
	Capabilities jsontext.Value `json:"capabilities"`
	ClientInfo   *clientInfo    `json:"clientInfo,omitempty"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type initializeResult struct {
	Capabilities jsontext.Value        `json:"capabilities"`
	ServerInfo   *initializeServerInfo `json:"serverInfo,omitempty"`
}

type initializeServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

type diagnostic struct {
	Range           lspRange         `json:"range"`
	Severity        int              `json:"severity,omitempty"`
	Code            string           `json:"code,omitempty"`
	CodeDescription *codeDescription `json:"codeDescription,omitempty"`
	Source          string           `json:"source,omitempty"`
	Message         string           `json:"message"`
}

type codeDescription struct {
	HRef string `json:"href"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type position struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int32  `json:"version"`
	Text       string `json:"text"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int32  `json:"version"`
}

type didOpenTextDocumentParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeTextDocumentParams struct {
	TextDocument   versionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []textDocumentContentChangeEvent `json:"contentChanges"`
}

type textDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

type didCloseTextDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type didSaveTextDocumentParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
}

type codeActionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Range        lspRange               `json:"range"`
	Context      codeActionContext      `json:"context"`
}

type codeActionContext struct {
	Diagnostics []diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
}

type codeAction struct {
	Title       string         `json:"title"`
	Kind        string         `json:"kind,omitempty"`
	IsPreferred bool           `json:"isPreferred,omitempty"`
	Diagnostics []diagnostic   `json:"diagnostics,omitempty"`
	Edit        *workspaceEdit `json:"edit,omitempty"`
}

type workspaceEdit struct {
	Changes map[string][]textEdit `json:"changes,omitempty"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

// Pull diagnostics types (textDocument/diagnostic).

type documentDiagnosticParams struct {
	TextDocument     textDocumentIdentifier `json:"textDocument"`
	PreviousResultID string                 `json:"previousResultId,omitempty"`
}

type fullDocumentDiagnosticReport struct {
	Kind     string       `json:"kind"`
	ResultID string       `json:"resultId,omitempty"`
	Items    []diagnostic `json:"items"`
}

type unchangedDocumentDiagnosticReport struct {
	Kind     string `json:"kind"`
	ResultID string `json:"resultId"`
}

// applyEdits applies LSP TextEdits to content using the TypeFox go-lsp library.
// Edits must be non-overlapping (LSP spec requirement).
func applyEdits(t *testing.T, uri, content string, edits []textEdit) string {
	t.Helper()
	m := tflsp.NewMapper(tflsp.DocumentURI(uri), []byte(content))
	tfEdits := make([]tflsp.TextEdit, 0, len(edits))
	for _, e := range edits {
		tfEdits = append(tfEdits, tflsp.TextEdit{
			Range: tflsp.Range{
				Start: tflsp.Position{Line: e.Range.Start.Line, Character: e.Range.Start.Character},
				End:   tflsp.Position{Line: e.Range.End.Line, Character: e.Range.End.Character},
			},
			NewText: e.NewText,
		})
	}
	out, _, err := tflsp.ApplyEdits(m, tfEdits)
	require.NoError(t, err, "ApplyEdits failed — edits may be overlapping (LSP spec violation)")
	return string(out)
}

// Formatting types (textDocument/formatting).

type documentFormattingParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Options      formattingOptions      `json:"options"`
}

type formattingOptions struct {
	TabSize                uint32 `json:"tabSize"`
	InsertSpaces           bool   `json:"insertSpaces"`
	TrimTrailingWhitespace bool   `json:"trimTrailingWhitespace,omitempty"`
	InsertFinalNewline     bool   `json:"insertFinalNewline,omitempty"`
	TrimFinalNewlines      bool   `json:"trimFinalNewlines,omitempty"`
}

// Execute command types (workspace/executeCommand).

type executeCommandParams struct {
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}
