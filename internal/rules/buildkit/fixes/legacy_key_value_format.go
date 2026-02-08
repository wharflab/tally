package fixes

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
)

// enrichLegacyKeyValueFormatFix adds auto-fix for BuildKit's LegacyKeyValueFormat rule.
// This replaces the legacy whitespace-separated format with the modern equals format:
//
//	ENV key value      → ENV key=value
//	ENV key multi val  → ENV key="multi val"
//	LABEL key value    → LABEL key=value
func enrichLegacyKeyValueFormatFix(v *rules.Violation, source []byte) {
	lineIdx := v.Location.Start.Line - 1
	line := getLine(source, lineIdx)
	if line == nil {
		return
	}

	it := ParseInstruction(line)

	// Find the instruction keyword (ENV or LABEL)
	var kw *Token
	for _, keyword := range []string{"ENV", "LABEL"} {
		if t := it.FindKeyword(keyword); t != nil {
			kw = t
			break
		}
	}
	if kw == nil {
		return
	}

	// Get everything after the keyword: "key value..."
	// We need to find the key and the value in the raw source line.
	afterKw := kw.End
	rest := line[afterKw:]

	// Skip whitespace after keyword
	trimmed := bytes.TrimLeft(rest, " \t")
	keyStart := afterKw + len(rest) - len(trimmed)

	if len(trimmed) == 0 {
		return
	}

	// Find the end of the key (first whitespace or '=' after key start)
	keyEnd := keyStart
	for keyEnd < len(line) && line[keyEnd] != ' ' && line[keyEnd] != '\t' && line[keyEnd] != '=' {
		keyEnd++
	}

	key := string(line[keyStart:keyEnd])
	if key == "" {
		return
	}

	// Find the value start (skip whitespace after key)
	valueStart := keyEnd
	for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\t') {
		valueStart++
	}

	if valueStart >= len(line) {
		// No value, just key. Fix: key → key=
		// This shouldn't normally happen for legacy format but handle gracefully.
		return
	}

	// Value is everything from valueStart to end of line (trimming trailing whitespace)
	value := strings.TrimRight(string(line[valueStart:]), " \t")

	// Build the replacement: key=value or key="value" if value contains spaces
	var newText string
	if strings.ContainsAny(value, " \t") {
		newText = fmt.Sprintf("%s=%q", key, value)
	} else {
		newText = key + "=" + value
	}

	v.SuggestedFix = &rules.SuggestedFix{
		Description: fmt.Sprintf("Replace legacy \"%s %s %s\" with \"%s %s\"",
			strings.ToUpper(kw.Value), key, value,
			strings.ToUpper(kw.Value), newText),
		Safety: rules.FixSafe,
		Edits: []rules.TextEdit{{
			// Replace from key start to end of line content
			Location: createEditLocation(v.Location.File, v.Location.Start.Line, keyStart, len(line)),
			NewText:  newText,
		}},
		IsPreferred: true,
	}
}
