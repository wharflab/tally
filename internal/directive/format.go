package directive

import (
	"regexp"
	"strings"
)

// reasonSeparator is the canonical separator between the rule list and the
// reason field, used when formatting new directives.
const reasonSeparator = ";reason="

// reasonPattern matches the reason separator in existing directive text.
// Mirrors the parser's regex: case-insensitive, optional whitespace around '='.
var reasonPattern = regexp.MustCompile(`(?i);reason\s*=\s*`)

// FormatNextLine produces a canonical next-line suppression directive comment:
//
//	# tally ignore=RULE1,RULE2[;reason=explanation]
func FormatNextLine(rules []string, reason string) string {
	return formatDirective("# tally ignore=", rules, reason)
}

// FormatGlobal produces a canonical global (file-level) suppression directive comment:
//
//	# tally global ignore=RULE1,RULE2[;reason=explanation]
func FormatGlobal(rules []string, reason string) string {
	return formatDirective("# tally global ignore=", rules, reason)
}

func formatDirective(prefix string, rules []string, reason string) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(strings.Join(rules, ","))
	if reason != "" {
		b.WriteString(reasonSeparator)
		b.WriteString(reason)
	}
	return b.String()
}

// AppendRuleEdit describes a text replacement to append a rule code to an
// existing directive line. Start and End are 0-based character offsets within
// the line; NewText is the replacement text for that range.
type AppendRuleEdit struct {
	Start   int
	End     int
	NewText string
}

// AppendRule computes the edit needed to append ruleCode to an existing
// directive line (e.g. "# tally ignore=DL3008" → insert ",DL3027").
//
// The edit inserts before ";reason=" if present, otherwise at end of line,
// trimming trailing whitespace before the insertion point.
func AppendRule(lineText, ruleCode string) AppendRuleEdit {
	// Find insertion point: before ;reason= (case-insensitive, flexible whitespace)
	// if present, otherwise at end of line.
	insertPos := len(lineText)
	if loc := reasonPattern.FindStringIndex(lineText); loc != nil {
		insertPos = loc[0]
	}

	// Trim trailing whitespace before insertion point.
	trimmed := insertPos
	for trimmed > 0 && (lineText[trimmed-1] == ' ' || lineText[trimmed-1] == '\t') {
		trimmed--
	}

	return AppendRuleEdit{
		Start:   trimmed,
		End:     insertPos,
		NewText: "," + ruleCode,
	}
}
