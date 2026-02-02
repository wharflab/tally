package fixes

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
)

// maintainerRegex extracts the maintainer value from a MAINTAINER instruction.
// Handles various formats like:
//   - MAINTAINER John Doe <john@example.com>
//   - MAINTAINER john@example.com
//   - MAINTAINER "John Doe <john@example.com>"
var maintainerRegex = regexp.MustCompile(`(?i)^\s*MAINTAINER\s+(.+?)\s*$`)

// enrichMaintainerDeprecatedFix adds auto-fix for BuildKit's MaintainerDeprecated rule.
// This replaces the deprecated MAINTAINER instruction with the OCI standard
// org.opencontainers.image.authors label.
//
// Example:
//
//	MAINTAINER John Doe <john@example.com>
//
// Becomes:
//
//	LABEL org.opencontainers.image.authors="John Doe <john@example.com>"
func enrichMaintainerDeprecatedFix(v *rules.Violation, source []byte) {
	// Get the line with MAINTAINER instruction (using 0-based index)
	lineIdx := v.Location.Start.Line - 1
	line := getLine(source, lineIdx)
	if line == nil {
		return
	}

	// Extract the maintainer value
	matches := maintainerRegex.FindSubmatch(line)
	if len(matches) < 2 {
		return
	}

	maintainerValue := strings.TrimSpace(string(matches[1]))
	if maintainerValue == "" {
		return
	}

	// Remove surrounding quotes only when they wrap the full value
	if len(maintainerValue) >= 2 {
		if (maintainerValue[0] == '"' && maintainerValue[len(maintainerValue)-1] == '"') ||
			(maintainerValue[0] == '\'' && maintainerValue[len(maintainerValue)-1] == '\'') {
			maintainerValue = maintainerValue[1 : len(maintainerValue)-1]
		}
	}

	// Create the replacement LABEL instruction (properly escaped)
	quoted := strconv.Quote(maintainerValue)
	newText := `LABEL org.opencontainers.image.authors=` + quoted

	v.SuggestedFix = &rules.SuggestedFix{
		Description: "Replace MAINTAINER with org.opencontainers.image.authors label",
		Safety:      rules.FixSafe,
		Edits: []rules.TextEdit{{
			// Replace the entire line (1-based line number)
			Location: createEditLocation(v.Location.File, v.Location.Start.Line, 0, len(line)),
			NewText:  newText,
		}},
		IsPreferred: true,
	}
}
