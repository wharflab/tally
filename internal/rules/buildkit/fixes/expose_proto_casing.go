package fixes

import (
	"strings"

	"github.com/wharflab/tally/internal/rules"
)

// enrichExposeProtoCasingFix adds auto-fix for BuildKit's ExposeProtoCasing rule.
// It lowercases protocols in EXPOSE instructions (e.g., "8080/TCP" → "8080/tcp").
func enrichExposeProtoCasingFix(v *rules.Violation, source []byte) {
	loc := v.Location

	lineIdx := loc.Start.Line - 1
	if lineIdx < 0 {
		return
	}

	line := getLine(source, lineIdx)
	if line == nil {
		return
	}

	// Parse the instruction to find port arguments
	it := ParseInstruction(line)
	exposeKw := it.FindKeyword("EXPOSE")
	if exposeKw == nil {
		return
	}

	args := it.Arguments()
	edits := make([]rules.TextEdit, 0, len(args))

	for _, arg := range args {
		// Each argument is a port spec like "8080/TCP" or "8080-8090/UDP"
		slashIdx := strings.IndexByte(arg.Value, '/')
		if slashIdx == -1 {
			continue
		}
		proto := arg.Value[slashIdx+1:]
		lower := strings.ToLower(proto)
		if proto == lower {
			continue
		}

		protoStart := arg.Start + slashIdx + 1
		protoEnd := arg.End
		edits = append(edits, rules.TextEdit{
			Location: createEditLocation(loc.File, loc.Start.Line, protoStart, protoEnd),
			NewText:  lower,
		})
	}

	if len(edits) == 0 {
		return
	}

	v.SuggestedFix = &rules.SuggestedFix{
		Description: "Change protocol to lowercase",
		Safety:      rules.FixSafe,
		Edits:       edits,
		IsPreferred: true,
	}
}
