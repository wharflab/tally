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
	"github.com/wharflab/tally/internal/rules"
)

const fixAllCodeActionKind = protocol.CodeActionKind("source.fixAll.tally")

func (s *Server) fixAllCodeAction(doc *Document, violations []rules.Violation) *protocol.CodeAction {
	if !hasFixAllCandidate(violations, s.resolveConfig(uriToPath(doc.URI))) {
		return nil
	}

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

func hasFixAllCandidate(violations []rules.Violation, cfg *config.Config) bool {
	fixModes := fix.BuildFixModes(cfg)

	for _, violation := range violations {
		if !fixAllCandidateAllowed(violation, fixModes) {
			continue
		}
		return true
	}

	return false
}

func fixAllCandidateAllowed(violation rules.Violation, fixModes map[string]fix.FixMode) bool {
	if violation.SuggestedFix == nil {
		return false
	}
	if violation.SuggestedFix.Safety > fix.FixSafe {
		return false
	}
	if !violation.SuggestedFix.NeedsResolve && len(violation.SuggestedFix.Edits) == 0 {
		return false
	}

	return fixModeAllowsSafeFix(violation.File(), violation.RuleCode, fixModes)
}

func fixModeAllowsSafeFix(_, ruleCode string, fixModes map[string]fix.FixMode) bool {
	mode := config.FixModeAlways
	if fixModes != nil {
		if configuredMode, ok := fixModes[ruleCode]; ok {
			mode = configuredMode
		}
	}

	switch mode {
	case config.FixModeNever, config.FixModeExplicit, config.FixModeUnsafeOnly:
		return false
	case config.FixModeAlways:
		return true
	default:
		return true
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
