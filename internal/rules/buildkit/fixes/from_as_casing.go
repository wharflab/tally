package fixes

import (
	"fmt"

	"github.com/wharflab/tally/internal/rules"
)

// enrichFromAsCasingFix adds auto-fix for BuildKit's FromAsCasing rule.
// This fixes mismatched casing between FROM and AS keywords.
//
// Example:
//
//	FROM alpine as builder  -> FROM alpine AS builder
//	from alpine AS builder  -> from alpine as builder
func enrichFromAsCasingFix(v *rules.Violation, source []byte) {
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

	// Parse the instruction using the tokenizer
	it := ParseInstruction(line)

	// Find the FROM keyword to determine its casing
	fromKeyword := it.FindKeyword("FROM")
	if fromKeyword == nil {
		return
	}
	fromIsUpper := fromKeyword.Value == "FROM"

	// Find the AS keyword
	asKeyword := it.FindKeyword("AS")
	if asKeyword == nil {
		return
	}

	currentAS := asKeyword.Value
	var newAS string
	if fromIsUpper {
		newAS = "AS"
	} else {
		newAS = "as"
	}

	// Skip if already correct
	if currentAS == newAS {
		return
	}

	v.SuggestedFix = &rules.SuggestedFix{
		Description: fmt.Sprintf("Change '%s' to '%s' to match FROM casing", currentAS, newAS),
		Safety:      rules.FixSafe,
		Edits: []rules.TextEdit{{
			Location: createEditLocation(loc.File, loc.Start.Line, asKeyword.Start, asKeyword.End),
			NewText:  newAS,
		}},
	}
}
