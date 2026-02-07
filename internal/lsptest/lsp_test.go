// Package lsptest implements black-box protocol tests for the tally LSP server.
//
// Each test launches tally lsp --stdio as a real subprocess and communicates
// over Content-Length-framed JSON-RPC on stdin/stdout. Coverage data from the
// subprocess is collected via GOCOVERDIR (same mechanism as internal/integration/).
package lsptest

import (
	"context"
	"encoding/json/jsontext"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gkampitakis/go-snaps/match"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLSP_Initialize(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	result := ts.initialize(t)

	// Snapshot the full server capabilities; version is dynamic.
	snaps.WithConfig(
		snaps.JSON(snaps.JSONConfig{
			SortKeys: true,
			Indent:   " ",
		}),
	).MatchStandaloneJSON(t, result, match.Any("serverInfo.version"))
}

func TestLSP_ShutdownExit(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	// Shutdown should succeed without error.
	ts.shutdown(t)

	// After exit notification, the subprocess should terminate.
	exited := make(chan error, 1)
	go func() { exited <- ts.wait() }()

	select {
	case <-exited:
		// Process exited (exit code may be non-zero due to jsonrpc2 handler teardown).
	case <-time.After(5 * time.Second):
		t.Fatal("server process did not exit after shutdown+exit")
	}
}

func TestLSP_DiagnosticsOnDidOpen(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-didopen/Dockerfile"
	ts.openDocument(t, uri, "FROM alpine:3.18\nMAINTAINER test@example.com\n")

	diag := ts.waitDiagnostics(t)

	// Snapshot the full diagnostics response.
	snaps.WithConfig(
		snaps.JSON(snaps.JSONConfig{
			SortKeys: true,
			Indent:   " ",
		}),
	).MatchStandaloneJSON(t, diag)
}

func TestLSP_DiagnosticsUpdatedOnDidChange(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-didchange/Dockerfile"

	// Open with MAINTAINER → expect diagnostics.
	ts.openDocument(t, uri, "FROM alpine:3.18\nMAINTAINER test@example.com\n")
	diag1 := ts.waitDiagnostics(t)
	require.NotEmpty(t, diag1.Diagnostics)

	hasMaintainer := func(diags []diagnostic) bool {
		for _, d := range diags {
			if d.Code == "buildkit/MaintainerDeprecated" {
				return true
			}
		}
		return false
	}
	assert.True(t, hasMaintainer(diag1.Diagnostics), "expected MaintainerDeprecated after open")

	// Change: remove MAINTAINER → diagnostics should no longer include it.
	ts.changeDocument(t, uri, 2, "FROM alpine:3.18\nLABEL maintainer=\"test@example.com\"\n")
	diag2 := ts.waitDiagnostics(t)
	assert.False(t, hasMaintainer(diag2.Diagnostics), "MaintainerDeprecated should be gone after change")
}

func TestLSP_DiagnosticsClearedOnClose(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-didclose/Dockerfile"

	ts.openDocument(t, uri, "FROM alpine:3.18\nMAINTAINER test@example.com\n")
	diag1 := ts.waitDiagnostics(t)
	require.NotEmpty(t, diag1.Diagnostics)

	// Close the document → server should publish empty diagnostics.
	ts.closeDocument(t, uri)
	diag2 := ts.waitDiagnostics(t)
	assert.Equal(t, uri, diag2.URI)
	assert.Empty(t, diag2.Diagnostics, "expected empty diagnostics after close")
}

func TestLSP_DiagnosticsOnDidSave(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-didsave/Dockerfile"

	// Open a clean file.
	ts.openDocument(t, uri, "FROM alpine:3.18\nRUN echo hello\n")
	diag1 := ts.waitDiagnostics(t)

	hasMaintainer := func(diags []diagnostic) bool {
		for _, d := range diags {
			if d.Code == "buildkit/MaintainerDeprecated" {
				return true
			}
		}
		return false
	}
	assert.False(t, hasMaintainer(diag1.Diagnostics))

	// Save with new text that includes MAINTAINER.
	ts.saveDocument(t, uri, "FROM alpine:3.18\nMAINTAINER test@example.com\n")
	diag2 := ts.waitDiagnostics(t)
	assert.True(t, hasMaintainer(diag2.Diagnostics), "expected MaintainerDeprecated after save")
}

func TestLSP_CodeAction(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-codeaction/Dockerfile"
	ts.openDocument(t, uri, "FROM alpine:3.18\nMAINTAINER test@example.com\n")

	diag := ts.waitDiagnostics(t)
	require.NotEmpty(t, diag.Diagnostics)

	// Find the MaintainerDeprecated diagnostic.
	idx := slices.IndexFunc(diag.Diagnostics, func(d diagnostic) bool {
		return d.Code == "buildkit/MaintainerDeprecated"
	})
	require.GreaterOrEqual(t, idx, 0, "expected MaintainerDeprecated diagnostic for code action test")
	maintainerDiag := &diag.Diagnostics[idx]

	// Request code actions for the MAINTAINER line.
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	var actions []codeAction
	err := ts.conn.Call(ctx, "textDocument/codeAction", &codeActionParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Range:        maintainerDiag.Range,
		Context: codeActionContext{
			Diagnostics: []diagnostic{*maintainerDiag},
		},
	}, &actions)
	require.NoError(t, err)

	// Snapshot the full code actions response.
	snaps.WithConfig(
		snaps.JSON(snaps.JSONConfig{
			SortKeys: true,
			Indent:   " ",
		}),
	).MatchStandaloneJSON(t, actions)
}

func TestLSP_PullDiagnosticsForOpenDocument(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-pull-open/Dockerfile"
	ts.openDocument(t, uri, "FROM alpine:3.18\nMAINTAINER test@example.com\n")

	// Drain push diagnostics from didOpen.
	ts.waitDiagnostics(t)

	// Request pull diagnostics.
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	var report fullDocumentDiagnosticReport
	err := ts.conn.Call(ctx, "textDocument/diagnostic", &documentDiagnosticParams{
		TextDocument: textDocumentIdentifier{URI: uri},
	}, &report)
	require.NoError(t, err)

	assert.Equal(t, "full", report.Kind)
	assert.NotEmpty(t, report.ResultID)
	assert.NotEmpty(t, report.Items, "expected diagnostics for Dockerfile with MAINTAINER")

	assert.True(t, slices.ContainsFunc(report.Items, func(d diagnostic) bool {
		return d.Code == "buildkit/MaintainerDeprecated"
	}), "expected MaintainerDeprecated in pull diagnostics")
}

func TestLSP_PullDiagnosticsFromDisk(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	// Write a Dockerfile to a temp directory so the server can read it from disk.
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	require.NoError(t, os.WriteFile(dockerfilePath, []byte("FROM alpine:3.18\nMAINTAINER test@example.com\n"), 0o644))

	// Construct a proper file:// URI. On Windows, paths like C:/... must
	// become /C:/... in the URL path to avoid C: being parsed as the host.
	uriPath := filepath.ToSlash(dockerfilePath)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := (&url.URL{Scheme: "file", Path: uriPath}).String()

	// Request pull diagnostics without opening the document.
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	var report fullDocumentDiagnosticReport
	err := ts.conn.Call(ctx, "textDocument/diagnostic", &documentDiagnosticParams{
		TextDocument: textDocumentIdentifier{URI: uri},
	}, &report)
	require.NoError(t, err)

	assert.Equal(t, "full", report.Kind)
	assert.NotEmpty(t, report.ResultID)
	assert.NotEmpty(t, report.Items, "expected diagnostics for Dockerfile on disk")
}

func TestLSP_PullDiagnosticsCacheUnchanged(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-pull-cache/Dockerfile"
	ts.openDocument(t, uri, "FROM alpine:3.18\nMAINTAINER test@example.com\n")

	// Drain push diagnostics from didOpen.
	ts.waitDiagnostics(t)

	// First pull: get full report with resultId.
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	var report1 fullDocumentDiagnosticReport
	err := ts.conn.Call(ctx, "textDocument/diagnostic", &documentDiagnosticParams{
		TextDocument: textDocumentIdentifier{URI: uri},
	}, &report1)
	require.NoError(t, err)
	require.Equal(t, "full", report1.Kind)
	require.NotEmpty(t, report1.ResultID)

	// Second pull with previousResultId: should get unchanged.
	ctx2, cancel2 := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel2()

	var report2 unchangedDocumentDiagnosticReport
	err = ts.conn.Call(ctx2, "textDocument/diagnostic", &documentDiagnosticParams{
		TextDocument:     textDocumentIdentifier{URI: uri},
		PreviousResultID: report1.ResultID,
	}, &report2)
	require.NoError(t, err)
	assert.Equal(t, "unchanged", report2.Kind)
	assert.Equal(t, report1.ResultID, report2.ResultID)
}

func TestLSP_Formatting(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-formatting/Dockerfile"
	ts.openDocument(t, uri, "FROM alpine:3.18\nMAINTAINER test@example.com\n")

	// Drain push diagnostics from didOpen.
	ts.waitDiagnostics(t)

	// Request formatting.
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	var edits []textEdit
	err := ts.conn.Call(ctx, "textDocument/formatting", &documentFormattingParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Options:      formattingOptions{TabSize: 4, InsertSpaces: true},
	}, &edits)
	require.NoError(t, err)

	// The MaintainerDeprecated fix is FixSafe, so formatting should replace
	// MAINTAINER with LABEL.
	require.NotEmpty(t, edits, "expected formatting edits for MAINTAINER → LABEL")

	// Verify at least one edit contains the LABEL instruction.
	found := slices.ContainsFunc(edits, func(e textEdit) bool {
		return strings.Contains(e.NewText, `LABEL org.opencontainers.image.authors="test@example.com"`)
	})
	assert.True(t, found, "expected LABEL replacement in formatting edits")
}

func TestLSP_FormattingConsistentCasing(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	// Real content from internal/integration/testdata/consistent-instruction-casing/Dockerfile.
	original := "# Test case for ConsistentInstructionCasing rule\nFROM alpine:3.18\nrun echo hello\nCOPY . /app\nworkdir /app\n"

	uri := "file:///tmp/test-formatting-casing/Dockerfile"
	ts.openDocument(t, uri, original)

	// Drain push diagnostics from didOpen.
	ts.waitDiagnostics(t)

	// Request formatting.
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	var edits []textEdit
	err := ts.conn.Call(ctx, "textDocument/formatting", &documentFormattingParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Options:      formattingOptions{TabSize: 4, InsertSpaces: true},
	}, &edits)
	require.NoError(t, err)
	require.NotEmpty(t, edits, "expected formatting edits for inconsistent casing")

	// Apply edits to original content to produce the fixed Dockerfile.
	// ApplyEdits also validates that edits are non-overlapping (LSP spec requirement).
	fixed := applyEdits(t, uri, original, edits)

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, fixed)
}

func TestLSP_FormattingRealWorld(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	// Real-world Dockerfile from internal/integration/testdata/benchmark-real-world-fix/.
	original, err := os.ReadFile("../integration/testdata/benchmark-real-world-fix/Dockerfile")
	require.NoError(t, err)

	uri := "file:///tmp/test-formatting-realworld/Dockerfile"
	ts.openDocument(t, uri, string(original))

	// Drain push diagnostics from didOpen.
	ts.waitDiagnostics(t)

	// Request formatting.
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	var edits []textEdit
	err = ts.conn.Call(ctx, "textDocument/formatting", &documentFormattingParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Options:      formattingOptions{TabSize: 4, InsertSpaces: true},
	}, &edits)
	require.NoError(t, err)
	require.NotEmpty(t, edits, "expected formatting edits for real-world Dockerfile")

	// Apply edits to original content to produce the fixed Dockerfile.
	// ApplyEdits also validates that edits are non-overlapping (LSP spec requirement).
	fixed := applyEdits(t, uri, string(original), edits)

	snaps.WithConfig(snaps.Ext(".Dockerfile")).MatchStandaloneSnapshot(t, fixed)
}

func TestLSP_FormattingNoChanges(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	uri := "file:///tmp/test-formatting-noop/Dockerfile"
	// A clean Dockerfile with no fixable issues.
	ts.openDocument(t, uri, "FROM alpine:3.18\nRUN echo hello\n")

	// Drain push diagnostics from didOpen.
	ts.waitDiagnostics(t)

	// Request formatting.
	ctx, cancel := context.WithTimeout(context.Background(), diagTimeout)
	defer cancel()

	var raw jsontext.Value
	err := ts.conn.Call(ctx, "textDocument/formatting", &documentFormattingParams{
		TextDocument: textDocumentIdentifier{URI: uri},
		Options:      formattingOptions{TabSize: 4, InsertSpaces: true},
	}, &raw)
	require.NoError(t, err)

	// When there are no changes, the server should return null.
	assert.True(t, raw == nil || string(raw) == "null", "expected null response for clean document, got: %s", string(raw))
}

func TestLSP_MethodNotFound(t *testing.T) {
	t.Parallel()
	ts := startTestServer(t)
	ts.initialize(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ts.conn.Call(ctx, "custom/nonExistentMethod", nil, nil)
	assert.Error(t, err, "unknown method should return an error")
}
