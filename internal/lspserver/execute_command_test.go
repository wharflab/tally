package lspserver

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourcegraph/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/tinovyatkin/tally/internal/lsp/protocol"
)

func TestParseApplyAllFixesArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       *[]any
		wantURI    string
		wantUnsafe bool
		wantOK     bool
	}{
		{
			name:       "nil args",
			args:       nil,
			wantURI:    "",
			wantUnsafe: false,
			wantOK:     false,
		},
		{
			name:       "empty args",
			args:       new([]any{}),
			wantURI:    "",
			wantUnsafe: false,
			wantOK:     false,
		},
		{
			name:       "string uri only",
			args:       new([]any{"file:///tmp/Dockerfile"}),
			wantURI:    "file:///tmp/Dockerfile",
			wantUnsafe: false,
			wantOK:     true,
		},
		{
			name:       "string uri with unsafe bool",
			args:       new([]any{"file:///tmp/Dockerfile", true}),
			wantURI:    "file:///tmp/Dockerfile",
			wantUnsafe: true,
			wantOK:     true,
		},
		{
			name:       "string uri with non-bool unsafe",
			args:       new([]any{"file:///tmp/Dockerfile", "nope"}),
			wantURI:    "file:///tmp/Dockerfile",
			wantUnsafe: false,
			wantOK:     true,
		},
		{
			name:       "string empty uri",
			args:       new([]any{""}),
			wantURI:    "",
			wantUnsafe: false,
			wantOK:     false,
		},
		{
			name:       "map uri only",
			args:       new([]any{map[string]any{"uri": "file:///tmp/Dockerfile"}}),
			wantURI:    "file:///tmp/Dockerfile",
			wantUnsafe: false,
			wantOK:     true,
		},
		{
			name:       "map uri with unsafe bool",
			args:       new([]any{map[string]any{"uri": "file:///tmp/Dockerfile", "unsafe": true}}),
			wantURI:    "file:///tmp/Dockerfile",
			wantUnsafe: true,
			wantOK:     true,
		},
		{
			name:       "map missing uri",
			args:       new([]any{map[string]any{"unsafe": true}}),
			wantURI:    "",
			wantUnsafe: false,
			wantOK:     false,
		},
		{
			name:       "map uri wrong type",
			args:       new([]any{map[string]any{"uri": 123}}),
			wantURI:    "",
			wantUnsafe: false,
			wantOK:     false,
		},
		{
			name:       "map uri empty",
			args:       new([]any{map[string]any{"uri": ""}}),
			wantURI:    "",
			wantUnsafe: false,
			wantOK:     false,
		},
		{
			name:       "map unsafe wrong type",
			args:       new([]any{map[string]any{"uri": "file:///tmp/Dockerfile", "unsafe": "nope"}}),
			wantURI:    "file:///tmp/Dockerfile",
			wantUnsafe: false,
			wantOK:     true,
		},
		{
			name:       "unsupported arg type",
			args:       new([]any{123}),
			wantURI:    "",
			wantUnsafe: false,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotURI, gotUnsafe, gotOK := parseApplyAllFixesArgs(tt.args)
			assert.Equal(t, tt.wantURI, gotURI)
			assert.Equal(t, tt.wantUnsafe, gotUnsafe)
			assert.Equal(t, tt.wantOK, gotOK)
		})
	}
}

func TestContentForURI_ReturnsOpenDocumentContent(t *testing.T) {
	t.Parallel()

	s := New()
	uri := "file:///tmp/Dockerfile"
	s.documents.Open(uri, "dockerfile", 1, "FROM alpine:3.18\n")

	content, err := s.contentForURI(uri)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine:3.18\n", string(content))
}

func TestContentForURI_ReadsFromDiskWhenNotOpen(t *testing.T) {
	t.Parallel()

	s := New()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "Dockerfile")
	require.NoError(t, os.WriteFile(path, []byte("FROM alpine:3.18\n"), 0o644))

	uri := fileURIFromPath(path)
	content, err := s.contentForURI(uri)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine:3.18\n", string(content))
}

func TestHandleExecuteCommand_NilParams(t *testing.T) {
	t.Parallel()

	s := New()
	result, err := s.handleExecuteCommand(nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHandleExecuteCommand_UnknownCommand(t *testing.T) {
	t.Parallel()

	s := New()
	result, err := s.handleExecuteCommand(&protocol.ExecuteCommandParams{Command: "unknown"})
	assert.Nil(t, result)

	var jerr *jsonrpc2.Error
	require.ErrorAs(t, err, &jerr)
	assert.EqualValues(t, jsonrpc2.CodeInvalidParams, jerr.Code)
	assert.Contains(t, jerr.Message, "unknown command")
}

func TestHandleExecuteCommand_InvalidArguments(t *testing.T) {
	t.Parallel()

	s := New()
	result, err := s.handleExecuteCommand(&protocol.ExecuteCommandParams{
		Command:   applyAllFixesCommand,
		Arguments: nil,
	})
	assert.Nil(t, result)

	var jerr *jsonrpc2.Error
	require.ErrorAs(t, err, &jerr)
	assert.EqualValues(t, jsonrpc2.CodeInvalidParams, jerr.Code)
	assert.Contains(t, jerr.Message, "invalid command arguments")
}

func TestHandleExecuteCommand_GracefullyReturnsNoEditsWhenFileCantBeRead(t *testing.T) {
	t.Parallel()

	s := New()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "Dockerfile") // file does not exist
	uri := fileURIFromPath(path)

	args := []any{uri}
	result, err := s.handleExecuteCommand(&protocol.ExecuteCommandParams{
		Command:   applyAllFixesCommand,
		Arguments: &args,
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHandleExecuteCommand_NoEditsWhenNoFixableChanges(t *testing.T) {
	t.Parallel()

	s := New()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "Dockerfile")
	uri := fileURIFromPath(path)
	s.documents.Open(uri, "dockerfile", 1, "FROM alpine:3.18\nRUN echo hello\n")

	args := []any{uri}
	result, err := s.handleExecuteCommand(&protocol.ExecuteCommandParams{
		Command:   applyAllFixesCommand,
		Arguments: &args,
	})
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestHandleExecuteCommand_ReturnsWorkspaceEdit_Unsafe(t *testing.T) {
	t.Parallel()

	s := New()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "Dockerfile")
	uri := fileURIFromPath(path)
	s.documents.Open(uri, "dockerfile", 1, "FROM alpine:3.18\nMAINTAINER test@example.com\n")

	args := []any{uri, true}
	result, err := s.handleExecuteCommand(&protocol.ExecuteCommandParams{
		Command:   applyAllFixesCommand,
		Arguments: &args,
	})
	require.NoError(t, err)

	edit, ok := result.(*protocol.WorkspaceEdit)
	require.True(t, ok, "expected *protocol.WorkspaceEdit result")
	require.NotNil(t, edit.Changes)

	edits := (*edit.Changes)[protocol.DocumentUri(uri)]
	require.NotEmpty(t, edits, "expected returned edits")
}

func fileURIFromPath(path string) string {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	return (&url.URL{Scheme: "file", Path: uriPath}).String()
}
