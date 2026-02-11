package lspserver

import (
	"bytes"
	"context"
	"path/filepath"

	protocol "github.com/tinovyatkin/tally/internal/lsp/protocol"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/fix"
	"github.com/tinovyatkin/tally/internal/linter"
	"github.com/tinovyatkin/tally/internal/processor"
)

const fixAllCodeActionKind = protocol.CodeActionKind("source.fixAll.tally")

func (s *Server) fixAllCodeAction(doc *Document) *protocol.CodeAction {
	edits := s.computeFixEdits(doc.URI, []byte(doc.Content), fix.FixSafe)
	if len(edits) == 0 {
		return nil
	}

	return &protocol.CodeAction{
		Title:       "Fix all auto-fixable issues",
		Kind:        new(fixAllCodeActionKind),
		IsPreferred: new(true),
		Edit: &protocol.WorkspaceEdit{
			Changes: new(map[protocol.DocumentUri][]*protocol.TextEdit{
				protocol.DocumentUri(doc.URI): edits,
			}),
		},
	}
}

func (s *Server) computeFixEdits(docURI string, content []byte, safety fix.FixSafety) []*protocol.TextEdit {
	input := s.lintInput(docURI, content)

	result, err := linter.LintFile(input)
	if err != nil {
		return nil
	}

	chain := linter.LSPProcessors()
	procCtx := processor.NewContext(
		map[string]*config.Config{input.FilePath: result.Config},
		result.Config,
		map[string][]byte{input.FilePath: content},
	)
	violations := chain.Process(result.Violations, procCtx)

	fixModes := fix.BuildFixModes(result.Config)
	fileKey := filepath.Clean(input.FilePath)
	fixer := &fix.Fixer{
		SafetyThreshold: safety,
		FixModes: map[string]map[string]fix.FixMode{
			fileKey: fixModes,
		},
	}
	fixResult, err := fixer.Apply(context.Background(), violations, map[string][]byte{input.FilePath: content})
	if err != nil {
		return nil
	}

	change := fixResult.Changes[fileKey]
	if change == nil || !change.HasChanges() || bytes.Equal(change.ModifiedContent, content) {
		return nil
	}

	return minimalTextEdit(content, change.ModifiedContent)
}
