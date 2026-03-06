package lspserver

import (
	"context"

	"github.com/wharflab/tally/internal/highlight"
	"github.com/wharflab/tally/internal/highlight/core"
	"github.com/wharflab/tally/internal/highlight/lspencode"
	protocol "github.com/wharflab/tally/internal/lsp/protocol"
)

func lspSemanticTokenTypes() []string {
	return append([]string(nil), lspencode.Legend.TokenTypes...)
}

func lspSemanticTokenModifiers() []string {
	return append([]string(nil), lspencode.Legend.TokenModifiers...)
}

func (s *Server) handleSemanticTokensFull(
	_ context.Context,
	params *protocol.SemanticTokensParams,
) (any, error) {
	uri := string(params.TextDocument.Uri)
	content, ok := s.semanticTokenContent(uri)
	if !ok {
		return &protocol.SemanticTokensOrNull{}, nil
	}

	doc := highlight.Analyze(uriToPath(uri), content)
	resultID := contentHash(content)
	return &protocol.SemanticTokensOrNull{
		SemanticTokens: &protocol.SemanticTokens{
			ResultId: &resultID,
			Data:     lspencode.Encode(doc.Tokens),
		},
	}, nil
}

func (s *Server) handleSemanticTokensRange(
	_ context.Context,
	params *protocol.SemanticTokensRangeParams,
) (any, error) {
	uri := string(params.TextDocument.Uri)
	content, ok := s.semanticTokenContent(uri)
	if !ok {
		return &protocol.SemanticTokensOrNull{}, nil
	}

	doc := highlight.Analyze(uriToPath(uri), content)
	startLine := int(params.Range.Start.Line)
	startCol := int(params.Range.Start.Character)
	endLine := int(params.Range.End.Line)
	endCol := int(params.Range.End.Character)
	if endLine < startLine || (endLine == startLine && endCol <= startCol) {
		return &protocol.SemanticTokensOrNull{
			SemanticTokens: &protocol.SemanticTokens{Data: nil},
		}, nil
	}

	filtered := core.FilterRange(doc.Tokens, startLine, startCol, endLine, endCol)
	return &protocol.SemanticTokensOrNull{
		SemanticTokens: &protocol.SemanticTokens{
			Data: lspencode.Encode(filtered),
		},
	}, nil
}

func (s *Server) semanticTokenContent(uri string) ([]byte, bool) {
	if doc := s.documents.Get(uri); doc != nil {
		return []byte(doc.Content), true
	}
	if isVirtualURI(uri) {
		return nil, false
	}
	return s.readValidatedFileContent(uriToPath(uri))
}
