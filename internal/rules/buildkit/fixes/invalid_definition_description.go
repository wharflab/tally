package fixes

import (
	"bytes"

	"github.com/tinovyatkin/tally/internal/rules"
)

// enrichInvalidDefinitionDescriptionFix adds auto-fix for BuildKit's InvalidDefinitionDescription rule.
// This rule triggers when a comment directly precedes a FROM or ARG instruction but doesn't
// follow the description format: `# <name> <description>`.
//
// The fix adds an empty line between the comment and the instruction to indicate
// that the comment is not intended as a description.
//
// Example (before):
//
//	# Some comment
//	FROM alpine AS builder
//
// Example (after):
//
//	# Some comment
//
//	FROM alpine AS builder
func enrichInvalidDefinitionDescriptionFix(v *rules.Violation, source []byte) {
	// The violation location points to the FROM/ARG line
	// We need to insert an empty line before it
	instructionLine := v.Location.Start.Line
	if instructionLine <= 1 {
		// No previous line to separate from
		return
	}

	// Check that there's a comment on the line before the instruction
	commentLineIdx := instructionLine - 2 // Convert to 0-based and go one line back
	lines := splitLines(source)
	if commentLineIdx < 0 || commentLineIdx >= len(lines) {
		return
	}

	commentLine := lines[commentLineIdx]
	trimmed := bytes.TrimSpace(commentLine)
	if len(trimmed) == 0 || trimmed[0] != '#' {
		// The previous line is not a comment, something is off
		return
	}

	// To insert an empty line, we add a newline at the end of the comment line.
	// This creates: "# comment\n\nFROM ..." from "# comment\nFROM ..."
	//
	// We insert at the end of the comment line (after its content, before existing newline).
	lineEnding := detectLineEnding(source)
	commentLineNum := instructionLine - 1 // 1-based line number of the comment
	commentLineLen := len(commentLine)

	v.SuggestedFix = &rules.SuggestedFix{
		Description: "Add empty line between comment and instruction",
		Safety:      rules.FixSafe,
		Edits: []rules.TextEdit{{
			// Insert at the end of the comment line
			Location: createEditLocation(v.Location.File, commentLineNum, commentLineLen, commentLineLen),
			NewText:  lineEnding,
		}},
		IsPreferred: true,
	}
}

// detectLineEnding detects the line ending style used in the source.
func detectLineEnding(source []byte) string {
	if bytes.Contains(source, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}
