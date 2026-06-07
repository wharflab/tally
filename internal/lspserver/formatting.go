package lspserver

import (
	"bytes"
	"context"
	"path/filepath"
	"unicode/utf8"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/linter"
	"github.com/wharflab/tally/internal/processor"
	"github.com/wharflab/tally/internal/rules"
)

// handleFormatting handles textDocument/formatting by applying safe auto-fixes.
//
// The response is computed by applying the fixes and then returning a minimal edit
// that transforms the original document into the fixed output (ESLint-style).
func (s *Server) handleFormatting(ctx context.Context, params *protocol.DocumentFormattingParams) (any, error) {
	doc := s.documents.Get(string(params.TextDocument.Uri))
	if doc == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no edits"
	}
	edits := s.formatRanges(ctx, doc, nil)
	if edits == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no edits"
	}
	return edits, nil
}

// handleRangeFormatting handles textDocument/rangeFormatting.
//
// LSP 3.0+: a single contiguous range to format. We re-use the formatting
// pipeline and filter fixes whose source span overlaps the requested range.
func (s *Server) handleRangeFormatting(ctx context.Context, params *protocol.DocumentRangeFormattingParams) (any, error) {
	doc := s.documents.Get(string(params.TextDocument.Uri))
	if doc == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no edits"
	}
	edits := s.formatRanges(ctx, doc, []protocol.Range{params.Range})
	if edits == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no edits"
	}
	return edits, nil
}

// handleRangesFormatting handles textDocument/rangesFormatting.
//
// LSP 3.18: multiple non-contiguous ranges to format in one request. This is
// what VS Code emits when "Format Selection" is invoked on a multi-cursor
// selection — sending one request per range would be wasteful here.
func (s *Server) handleRangesFormatting(ctx context.Context, params *protocol.DocumentRangesFormattingParams) (any, error) {
	doc := s.documents.Get(string(params.TextDocument.Uri))
	if doc == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no edits"
	}
	if len(params.Ranges) == 0 {
		return nil, nil //nolint:nilnil // nothing to format
	}
	edits := s.formatRanges(ctx, doc, params.Ranges)
	if edits == nil {
		return nil, nil //nolint:nilnil // LSP: null result is valid for "no edits"
	}
	return edits, nil
}

// formatRanges runs the lint/fix pipeline against the document and returns
// the resulting edits. When ranges is non-nil, only violations whose source
// span overlaps one of those ranges are fixed — this maps "Format Selection"
// onto "fix what tally would fix within the selection".
//
// Returns nil when there is nothing to do; the caller must convert nil to
// the LSP null result.
func (s *Server) formatRanges(ctx context.Context, doc *Document, ranges []protocol.Range) []*protocol.TextEdit {
	content := []byte(doc.Content)
	input := s.lintInput(doc.URI, content)
	fileKey := filepath.Clean(input.FilePath)

	// 1. Lint + filter: reuse shared pipeline.
	result, err := linter.LintFileContext(ctx, input)
	if err != nil {
		return nil
	}

	chain := linter.LSPProcessors()
	procCtx := processor.NewContext(
		map[string]*config.Config{fileKey: result.Config},
		result.Config,
		map[string][]byte{fileKey: content},
	)
	violations := chain.Process(result.Violations, procCtx)

	// 2. Restrict to violations that overlap the requested ranges. For range
	// formatting we additionally drop async (resolver-backed) fixes — those are
	// global structural transforms (e.g. inserting blank lines between every
	// instruction) which would otherwise reflow the whole document even when
	// only a single line is selected. The user expects "format this selection"
	// to only touch the selection.
	if ranges != nil {
		filtered := make([]rules.Violation, 0, len(violations))
		for _, v := range violations {
			pf := v.PreferredFix()
			if pf == nil || pf.NeedsResolve {
				continue
			}
			vRange := violationRange(v)
			for _, r := range ranges {
				if rangesOverlap(vRange, r) || (rangeIsEmpty(r) && rangeContainsPosition(vRange, r.Start)) {
					filtered = append(filtered, v)
					break
				}
			}
		}
		if len(filtered) == 0 {
			return nil
		}
		violations = filtered
	}

	// 3. Apply style-safe fixes via existing fix infrastructure.
	// The fixer handles conflict resolution and ordering and respects per-rule fix modes.
	fixModes := fix.BuildFixModes(result.Config)
	fixer := &fix.Fixer{
		SafetyThreshold: fix.FixSafe,
		FixModes: map[string]map[string]fix.FixMode{
			fileKey: fixModes,
		},
	}
	fixResult, err := fixer.Apply(ctx, violations, map[string][]byte{fileKey: content})
	if err != nil {
		return nil
	}

	change := fixResult.Changes[fileKey]
	if change == nil || !change.HasChanges() || bytes.Equal(change.ModifiedContent, content) {
		return nil
	}

	edits := minimalTextEdit(content, change.ModifiedContent)
	if len(edits) == 0 {
		return nil
	}
	return edits
}

func minimalTextEdit(original, modified []byte) []*protocol.TextEdit {
	start, end, replacement, ok := minimalReplacement(original, modified)
	if !ok {
		return nil
	}

	return []*protocol.TextEdit{
		{
			Range: protocol.Range{
				Start: positionAtOffset(original, start),
				End:   positionAtOffset(original, end),
			},
			NewText: string(replacement),
		},
	}
}

func minimalReplacement(original, modified []byte) (int, int, []byte, bool) {
	if bytes.Equal(original, modified) {
		return 0, 0, nil, false
	}

	prefix := 0
	for prefix < len(original) && prefix < len(modified) {
		r1, size1 := utf8.DecodeRune(original[prefix:])
		r2, size2 := utf8.DecodeRune(modified[prefix:])
		if r1 != r2 || size1 != size2 || !bytes.Equal(original[prefix:prefix+size1], modified[prefix:prefix+size2]) {
			break
		}
		prefix += size1
	}

	origEnd := len(original)
	modEnd := len(modified)
	for origEnd > prefix && modEnd > prefix {
		r1, size1 := utf8.DecodeLastRune(original[:origEnd])
		r2, size2 := utf8.DecodeLastRune(modified[:modEnd])
		if origEnd-size1 < prefix || modEnd-size2 < prefix {
			break
		}
		if r1 != r2 || size1 != size2 || !bytes.Equal(original[origEnd-size1:origEnd], modified[modEnd-size2:modEnd]) {
			break
		}
		origEnd -= size1
		modEnd -= size2
	}

	start := prefix
	end := origEnd
	replStart := prefix
	replEnd := modEnd
	replacement := modified[replStart:replEnd]
	return start, end, replacement, true
}

func positionAtOffset(content []byte, offset int) protocol.Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}

	line := uint32(0)
	utf16Char := 0

	for i := 0; i < offset; {
		r, size := utf8.DecodeRune(content[i:])
		next := i + size
		// offset is a byte offset; don't decode past it.
		if next > offset {
			break
		}

		if r == '\n' {
			line++
			utf16Char = 0
			i = next
			continue
		}

		switch {
		case r == utf8.RuneError && size == 1:
			utf16Char++
		case r > 0xFFFF:
			utf16Char += 2 // surrogate pair in UTF-16
		default:
			utf16Char++
		}
		i = next
	}

	return protocol.Position{Line: line, Character: clampUint32(utf16Char)}
}
