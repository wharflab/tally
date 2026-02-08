// Package sourcemap provides utilities for working with source code locations,
// snippet extraction, and line-based operations.
//
// This package bridges BuildKit's AST positions with our output requirements
// (snippets for diagnostics, comment extraction for inline directives).
package sourcemap

import (
	"bytes"
	"slices"
	"strings"
)

// SourceMap provides efficient access to source code by line.
// It precomputes line boundaries for fast snippet extraction.
//
// All line numbers are 0-based (matching BuildKit/LSP conventions).
type SourceMap struct {
	// source is the raw source content.
	source []byte

	// lines are the individual lines (without line endings).
	lines []string

	// lineOffsets[i] is the byte offset where line i starts in source.
	// Used for computing column positions from byte offsets.
	lineOffsets []int
}

// New creates a SourceMap from source content.
// Lines are split on \n (handles both \n and \r\n).
func New(source []byte) *SourceMap {
	// Split into lines, preserving empty lines
	rawLines := bytes.Split(source, []byte{'\n'})
	lines := make([]string, len(rawLines))
	lineOffsets := make([]int, len(rawLines))

	offset := 0
	for i, line := range rawLines {
		lineOffsets[i] = offset
		// Trim \r from line endings (for Windows CRLF)
		lines[i] = strings.TrimSuffix(string(line), "\r")
		// Next line starts after this line + newline character
		offset += len(line) + 1
	}

	return &SourceMap{
		source:      source,
		lines:       lines,
		lineOffsets: lineOffsets,
	}
}

// Lines returns all lines (without line endings).
// The returned slice should not be modified.
func (sm *SourceMap) Lines() []string {
	return sm.lines
}

// LineCount returns the total number of lines.
func (sm *SourceMap) LineCount() int {
	return len(sm.lines)
}

// Line returns the text of a specific line (0-based).
// Returns empty string if line is out of range.
func (sm *SourceMap) Line(line int) string {
	if line < 0 || line >= len(sm.lines) {
		return ""
	}
	return sm.lines[line]
}

// LineOffset returns the byte offset where a line starts (0-based).
// Returns -1 if line is out of range.
func (sm *SourceMap) LineOffset(line int) int {
	if line < 0 || line >= len(sm.lineOffsets) {
		return -1
	}
	return sm.lineOffsets[line]
}

// Snippet extracts a range of lines as a single string.
// Both startLine and endLine are 0-based and inclusive.
// Returns empty string if range is invalid.
//
// Example:
//
//	sm.Snippet(2, 4) // Returns lines 2, 3, and 4 joined with newlines
func (sm *SourceMap) Snippet(startLine, endLine int) string {
	// Clamp to valid range
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(sm.lines) {
		endLine = len(sm.lines) - 1
	}
	if startLine > endLine || startLine >= len(sm.lines) {
		return ""
	}

	return strings.Join(sm.lines[startLine:endLine+1], "\n")
}

// SnippetAround extracts context lines around a target line.
// Returns (contextBefore + target + contextAfter) lines as a single string.
// The before/after counts are clamped to available lines.
//
// Example:
//
//	sm.SnippetAround(5, 2, 2) // Returns lines 3-7 (5 ± 2)
func (sm *SourceMap) SnippetAround(line, before, after int) string {
	startLine := line - before
	endLine := line + after
	return sm.Snippet(startLine, endLine)
}

// Source returns the raw source content.
// The returned slice should not be modified.
func (sm *SourceMap) Source() []byte {
	return sm.source
}

// Comment represents a comment extracted from source.
// Comments in Dockerfiles start with # and extend to end of line.
type Comment struct {
	// Line is the 0-based line number where the comment appears.
	Line int

	// Text is the comment text including the # prefix.
	// Leading whitespace before # is trimmed.
	Text string

	// IsDirective indicates if this looks like a directive comment.
	// True if the comment matches patterns like:
	//   # tally ignore=...
	//   # hadolint ignore=...
	//   # check=skip=...
	//   # syntax=...
	//   # escape=...
	IsDirective bool
}

// Comments extracts all comments from the source.
// This includes both standalone comment lines and comments associated with AST nodes.
// Comments are returned in line order.
//
// Note: This only extracts top-level comments (lines starting with #).
// Comments within instruction arguments are not extracted.
func (sm *SourceMap) Comments() []Comment {
	var comments []Comment

	for i, line := range sm.lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			comments = append(comments, Comment{
				Line:        i,
				Text:        trimmed,
				IsDirective: isDirectiveComment(trimmed),
			})
		}
	}

	return comments
}

// isDirectiveComment checks if a comment looks like a directive.
// These are special comments that control linter behavior.
func isDirectiveComment(text string) bool {
	// Remove # prefix and trim
	content := strings.TrimSpace(strings.TrimPrefix(text, "#"))
	lower := strings.ToLower(content)

	// Check for known directive patterns
	// These must be followed by a space or = to avoid false positives like "tallyho"
	directives := []string{
		"tally ",          // # tally ignore=... or # tally global ignore=...
		"hadolint ",       // # hadolint ignore=...
		"check=",          // # check=skip=... (buildx format)
		"syntax=",         // # syntax=docker/dockerfile:1
		"escape=",         // # escape=`
		"parser-dialect=", // Parser directive (BuildKit)
	}

	return slices.ContainsFunc(directives, func(directive string) bool {
		return strings.HasPrefix(lower, directive)
	})
}

// CommentsForLine returns all comments that appear immediately before a line.
// This matches BuildKit's PrevComment behavior where comments are associated
// with the following instruction.
//
// Example: For line 5, this returns comments from lines 3-4 if:
//   - Line 3: # comment one
//   - Line 4: # comment two
//   - Line 5: FROM alpine
func (sm *SourceMap) CommentsForLine(line int) []Comment {
	var comments []Comment

	// Walk backwards from the line before target
	for i := line - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(sm.lines[i])
		if trimmed == "" {
			// Empty line breaks the comment block
			break
		}
		if !strings.HasPrefix(trimmed, "#") {
			// Non-comment, non-empty line breaks the block
			break
		}
		// Append in reverse order (will be reversed at the end)
		comments = append(comments, Comment{
			Line:        i,
			Text:        trimmed,
			IsDirective: isDirectiveComment(trimmed),
		})
	}

	// Reverse to maintain line order (we collected in reverse)
	for i, j := 0, len(comments)-1; i < j; i, j = i+1, j-1 {
		comments[i], comments[j] = comments[j], comments[i]
	}

	return comments
}
