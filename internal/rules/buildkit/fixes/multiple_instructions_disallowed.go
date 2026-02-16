package fixes

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/wharflab/tally/internal/rules"
)

// multiInstrRegex extracts the instruction name from the violation message.
// Example: "Multiple CMD instructions should not be used..."
var multiInstrRegex = regexp.MustCompile(`^Multiple (\w+) instructions`)

// enrichMultipleInstructionsDisallowedFix adds auto-fix for BuildKit's MultipleInstructionsDisallowed rule.
// Docker ignores all but the last CMD/ENTRYPOINT in a stage, so the fix comments out the
// earlier (ignored) instructions.
//
// Cross-rule interactions:
//   - Covers CMD (DL4003), ENTRYPOINT (DL4004), and HEALTHCHECK (DL3012).
//   - JSONArgsRecommended, ConsistentInstructionCasing: Commented lines are excluded from these checks.
//     Fix uses priority -1 to apply before those cosmetic fixes on the same line.
//
// Example:
//
//	CMD echo "first"
//
// Becomes:
//
//	# [commented out by tally - Docker will ignore all but last CMD]: CMD echo "first"
func enrichMultipleInstructionsDisallowedFix(v *rules.Violation, source []byte) {
	lineIdx := v.Location.Start.Line - 1
	line := getLine(source, lineIdx)
	if line == nil {
		return
	}

	// Extract instruction name from the message
	matches := multiInstrRegex.FindStringSubmatch(v.Message)
	if len(matches) < 2 {
		return
	}
	instrName := matches[1]

	// Verify the line actually contains the instruction
	trimmed := bytes.TrimSpace(line)
	if !strings.HasPrefix(strings.ToUpper(string(trimmed)), instrName) {
		return
	}

	// Build the comment replacement
	commentedLine := "# [commented out by tally - Docker will ignore all but last " + instrName + "]: " + string(line)

	v.SuggestedFix = &rules.SuggestedFix{
		Description: "Comment out duplicate " + instrName + " instruction (only the last one takes effect)",
		Safety:      rules.FixSafe,
		// Priority -1: must apply before cosmetic fixes (casing at 0, JSON form at 0) on the same line.
		// Commenting out a dead instruction makes other fixes on that line moot.
		Priority: -1,
		Edits: []rules.TextEdit{{
			Location: createEditLocation(v.Location.File, v.Location.Start.Line, 0, len(line)),
			NewText:  commentedLine,
		}},
		IsPreferred: true,
	}
}
