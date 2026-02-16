package fixes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wharflab/tally/internal/rules"
)

// casingMessageRegex extracts the instruction name and expected casing from BuildKit's message.
// Message format: "Command 'run' should match the case of the command majority (uppercase)"
var casingMessageRegex = regexp.MustCompile(`Command '([^']+)' should match the case of the command majority \((\w+)\)`)

// enrichConsistentInstructionCasingFix adds auto-fix for BuildKit's ConsistentInstructionCasing rule.
// It changes the instruction keyword to match the majority casing in the Dockerfile.
//
// Example:
//
//	run echo hello     -> RUN echo hello   (when majority is uppercase)
//	WORKDIR /app       -> workdir /app     (when majority is lowercase)
func enrichConsistentInstructionCasingFix(v *rules.Violation, source []byte) {
	// Extract instruction name and expected casing from message
	matches := casingMessageRegex.FindStringSubmatch(v.Message)
	if len(matches) < 3 {
		return
	}

	instructionName := matches[1]
	expectedCasing := matches[2] // "uppercase" or "lowercase"

	loc := v.Location

	// Get the line (Location uses 1-based line numbers)
	lineIdx := loc.Start.Line - 1
	if lineIdx < 0 {
		return
	}

	line := getLine(source, lineIdx)
	if line == nil {
		return
	}

	// Parse the instruction to find the keyword
	it := ParseInstruction(line)

	// Find the instruction keyword that matches (case-insensitive)
	var keywordToken *Token
	for _, tok := range it.tokens {
		if tok.Type == TokenKeyword && strings.EqualFold(tok.Value, instructionName) {
			keywordToken = &tok
			break
		}
	}

	if keywordToken == nil {
		return
	}

	// Determine the new text based on expected casing
	var newText string
	if expectedCasing == "lowercase" {
		newText = strings.ToLower(instructionName)
	} else {
		newText = strings.ToUpper(instructionName)
	}

	// Skip if already correct (shouldn't happen, but be defensive)
	if keywordToken.Value == newText {
		return
	}

	v.SuggestedFix = &rules.SuggestedFix{
		Description: fmt.Sprintf("Change '%s' to '%s' to match majority casing", keywordToken.Value, newText),
		Safety:      rules.FixSafe,
		Edits: []rules.TextEdit{{
			Location: createEditLocation(loc.File, loc.Start.Line, keywordToken.Start, keywordToken.End),
			NewText:  newText,
		}},
		IsPreferred: true,
	}
}
