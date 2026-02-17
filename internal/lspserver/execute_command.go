package lspserver

import (
	"context"
	"os"

	"golang.org/x/exp/jsonrpc2"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"

	"github.com/wharflab/tally/internal/fix"
)

const applyAllFixesCommand = "tally.applyAllFixes"

func (s *Server) handleExecuteCommand(ctx context.Context, params *protocol.ExecuteCommandParams) (any, error) {
	if params == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid
	}
	if params.Command != applyAllFixesCommand {
		return nil, jsonrpc2.NewError(int64(protocol.ErrorCodeInvalidParams), "unknown command: "+params.Command)
	}

	uri, unsafe, ok := parseApplyAllFixesArgs(params.Arguments)
	if !ok {
		return nil, jsonrpc2.NewError(int64(protocol.ErrorCodeInvalidParams), "invalid command arguments")
	}

	content, err := s.contentForURI(uri)
	if err != nil {
		return nil, nil //nolint:nilnil,nilerr // gracefully return no edits when the file can't be read
	}

	safety := fix.FixSafe
	if unsafe {
		safety = fix.FixUnsafe
	}

	edits := s.computeFixEdits(ctx, uri, content, safety)
	if len(edits) == 0 {
		return nil, nil //nolint:nilnil // no changes
	}

	return &protocol.WorkspaceEdit{
		Changes: new(map[protocol.DocumentUri][]*protocol.TextEdit{
			protocol.DocumentUri(uri): edits,
		}),
	}, nil
}

func parseApplyAllFixesArgs(args *[]any) (string, bool, bool) {
	if args == nil || len(*args) == 0 {
		return "", false, false
	}

	switch v := (*args)[0].(type) {
	case string:
		uri := v
		unsafe := false
		if len(*args) > 1 {
			if b, ok := (*args)[1].(bool); ok {
				unsafe = b
			}
		}
		return uri, unsafe, uri != ""
	case map[string]any:
		rawURI, ok := v["uri"]
		if !ok {
			return "", false, false
		}
		uri, ok := rawURI.(string)
		if !ok || uri == "" {
			return "", false, false
		}
		unsafe := false
		if rawUnsafe, ok := v["unsafe"]; ok {
			if b, ok := rawUnsafe.(bool); ok {
				unsafe = b
			}
		}
		return uri, unsafe, true
	default:
		return "", false, false
	}
}

func (s *Server) contentForURI(uri string) ([]byte, error) {
	if doc := s.documents.Get(uri); doc != nil {
		return []byte(doc.Content), nil
	}
	return os.ReadFile(uriToPath(uri))
}
