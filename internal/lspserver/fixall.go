package lspserver

import (
	"bytes"
	"context"
	"path/filepath"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/linter"
	"github.com/wharflab/tally/internal/processor"
)

const fixAllCodeActionKind = protocol.CodeActionKind("source.fixAll.tally")

func (s *Server) fixAllCodeAction(doc *Document) *protocol.CodeAction {
	args := []any{
		map[string]any{"uri": doc.URI},
	}
	return &protocol.CodeAction{
		Title:       "Fix all auto-fixable issues",
		Kind:        new(fixAllCodeActionKind),
		IsPreferred: new(true),
		Command: &protocol.Command{
			Title:     "Fix all auto-fixable issues",
			Command:   applyAllFixesCommand,
			Arguments: &args,
		},
	}
}

func (s *Server) computeFixEdits(ctx context.Context, docURI string, content []byte, safety fix.FixSafety) []*protocol.TextEdit {
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
	fixResult, err := fixer.Apply(ctx, violations, map[string][]byte{input.FilePath: content})
	if err != nil {
		return nil
	}

	change := fixResult.Changes[fileKey]
	if change == nil || !change.HasChanges() || bytes.Equal(change.ModifiedContent, content) {
		return nil
	}

	return minimalTextEdit(content, change.ModifiedContent)
}
