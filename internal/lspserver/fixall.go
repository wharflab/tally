package lspserver

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"

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
	return slices.ContainsFunc(violations, func(v rules.Violation) bool {
		return fixAllCandidateAllowed(v, fixModes)
	})
}

func fixAllCandidateAllowed(violation rules.Violation, fixModes map[string]fix.FixMode) bool {
	pf := violation.PreferredFix()
	if pf == nil {
		return false
	}
	if pf.Safety > fix.FixSafe {
		return false
	}
	if !pf.NeedsResolve && len(pf.Edits) == 0 {
		return false
	}

	return fixModeAllowsSafeFix(violation.RuleCode, fixModes)
}

func fixModeAllowsSafeFix(ruleCode string, fixModes map[string]fix.FixMode) bool {
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

const maxFixIterations = 10

// computeFixEdits computes text edits to fix all auto-fixable violations.
// In "all" mode (fixAllModeAll), it iteratively re-lints and re-fixes until
// convergence or maxFixIterations. In "problems" mode (fixAllModeProblems),
// it applies a single pass (current behavior).
func (s *Server) computeFixEdits(
	ctx context.Context,
	docURI string,
	content []byte,
	safety fix.FixSafety,
	mode string,
) []*protocol.TextEdit {
	if mode == fixAllModeAll {
		return s.computeFixEditsIterative(ctx, docURI, content, safety)
	}
	filePath := uriToPath(docURI)
	cfg := s.resolveConfig(filePath)
	fixModes := fix.BuildFixModes(cfg)
	fixed, err := computeFixedContent(ctx, filePath, content, cfg, fixModes, safety)
	if err != nil || fixed == nil || bytes.Equal(fixed, content) {
		return nil
	}
	return minimalTextEdit(content, fixed)
}

// computeFixEditsIterative runs lint+fix in a loop until the content stabilizes
// or maxFixIterations is reached. Returns minimal text edits from original to final.
//
// Config and fix modes are resolved once before the loop — they don't change
// between iterations (only the content does).
func (s *Server) computeFixEditsIterative(ctx context.Context, docURI string, content []byte, safety fix.FixSafety) []*protocol.TextEdit {
	filePath := uriToPath(docURI)
	cfg := s.resolveConfig(filePath)
	fixModes := fix.BuildFixModes(cfg)

	current := content
	for range maxFixIterations {
		if ctx.Err() != nil {
			return nil
		}
		next, err := computeFixedContent(ctx, filePath, current, cfg, fixModes, safety)
		if err != nil {
			return nil // don't return partially-fixed content on error
		}
		if next == nil || bytes.Equal(next, current) {
			break // converged
		}
		current = next
	}
	if bytes.Equal(current, content) {
		return nil
	}
	return minimalTextEdit(content, current)
}

// computeFixedContent runs a single lint+fix pass and returns the modified content.
// Config and fixModes are pre-resolved by the caller so they aren't recomputed
// on each iteration.
//
// Returns (nil, nil) when there is nothing to fix (convergence).
// Returns (nil, err) when linting or fixing fails.
func computeFixedContent(
	ctx context.Context,
	filePath string,
	content []byte,
	cfg *config.Config,
	fixModes map[string]fix.FixMode,
	safety fix.FixSafety,
) ([]byte, error) {
	input := linter.Input{
		FilePath: filePath,
		Content:  content,
		Config:   cfg,
	}

	result, err := linter.LintFileContext(ctx, input)
	if err != nil {
		return nil, err
	}

	chain := linter.LSPProcessors()
	procCtx := processor.NewContext(
		map[string]*config.Config{filePath: result.Config},
		result.Config,
		map[string][]byte{filePath: content},
	)
	violations := chain.Process(result.Violations, procCtx)

	fileKey := filepath.Clean(filePath)
	fixer := &fix.Fixer{
		SafetyThreshold: safety,
		FixModes: map[string]map[string]fix.FixMode{
			fileKey: fixModes,
		},
	}
	fixResult, err := fixer.Apply(ctx, violations, map[string][]byte{filePath: content})
	if err != nil {
		return nil, err
	}

	change := fixResult.Changes[fileKey]
	if change == nil || !change.HasChanges() || bytes.Equal(change.ModifiedContent, content) {
		return nil, nil
	}

	return change.ModifiedContent, nil
}
