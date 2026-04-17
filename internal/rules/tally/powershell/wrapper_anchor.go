package powershell

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/sourcemap"
)

// isMultiLineInstruction reports whether the given source range spans more
// than one physical line. Used by SHELL-line fixes that scan a single line
// for brackets/quotes and need to bail out on continuation-split instructions
// (backslash or backtick) to avoid producing invalid edit locations.
//
// Falls back to sm.ResolveEndLine when the parser-reported end line matches
// the start line, which covers continuation cases where the parser collapses
// the range (common for RUN; rare but possible for SHELL).
func isMultiLineInstruction(sm *sourcemap.SourceMap, r parser.Range) bool {
	if r.End.Line > r.Start.Line {
		return true
	}
	if sm == nil {
		return false
	}
	return sm.ResolveEndLine(r.End.Line) > r.Start.Line
}

// wrapperInsertionPoint locates the (line, column) offset inside a RUN's
// source region where the inner script of an explicit powershell/pwsh
// -Command wrapper begins. line is a 0-based offset from the RUN's first
// location line; col is 0-based. The search is anchored past the executable
// token so we never match the "RUN powershell" prefix.
//
// Shared by tally/powershell/error-action-preference and
// tally/powershell/progress-preference, which both inject preamble text at
// this anchor with a zero-width insertion.
func wrapperInsertionPoint(
	sm *sourcemap.SourceMap,
	run *instructions.RunCommand,
	invocation explicitPowerShellInvocation,
) (line, col int, ok bool) {
	if sm == nil || len(run.Location()) == 0 {
		return 0, 0, false
	}

	startLine := run.Location()[0].Start.Line
	endLine := run.Location()[len(run.Location())-1].End.Line

	source := sm.Snippet(startLine-1, endLine-1)
	if source == "" {
		return 0, 0, false
	}

	exeLower := strings.ToLower(invocation.executable)
	anchorIdx := strings.Index(strings.ToLower(source), exeLower)
	if anchorIdx < 0 {
		return 0, 0, false
	}
	searchStart := anchorIdx + len(exeLower)

	firstToken := firstNonWhitespaceWord(invocation.script)
	if firstToken == "" {
		return 0, 0, false
	}
	relIdx := strings.Index(source[searchStart:], firstToken)
	if relIdx < 0 {
		return 0, 0, false
	}
	insertByte := searchStart + relIdx

	line, col = sourcemap.ByteToLineCol(source, insertByte)
	return line, col, true
}
