package directive

import "strings"

// reasonSeparator is the separator between the rule list and the reason field.
// Used by both the parser (regex) and the formatter (this file).
const reasonSeparator = ";reason="

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
	// Find insertion point: before ;reason= if present, otherwise at end.
	insertPos := len(lineText)
	if idx := strings.Index(lineText, reasonSeparator); idx >= 0 {
		insertPos = idx
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
