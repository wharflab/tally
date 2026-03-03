package tally

import (
	"fmt"
	"strings"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
)

// NoMultiSpacesRuleCode is the full rule code for the no-multi-spaces rule.
const NoMultiSpacesRuleCode = rules.TallyRulePrefix + "no-multi-spaces"

// NoMultiSpacesRule implements the no-multi-spaces linting rule.
type NoMultiSpacesRule struct {
	schema map[string]any
}

// NewNoMultiSpacesRule creates a new no-multi-spaces rule instance.
func NewNoMultiSpacesRule() *NoMultiSpacesRule {
	schema, err := configutil.RuleSchema(NoMultiSpacesRuleCode)
	if err != nil {
		panic(err)
	}
	return &NoMultiSpacesRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *NoMultiSpacesRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoMultiSpacesRuleCode,
		Name:            "No Multiple Spaces",
		Description:     "Disallows multiple consecutive spaces within Dockerfile instructions",
		DocURL:          rules.TallyDocURL(NoMultiSpacesRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		FixPriority:     10,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *NoMultiSpacesRule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *NoMultiSpacesRule) DefaultConfig() any {
	return nil
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *NoMultiSpacesRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(NoMultiSpacesRuleCode, config)
}

// Check runs the no-multi-spaces rule.
//
// Each line with violations produces exactly one Violation whose SuggestedFix
// contains one TextEdit per contiguous run of multiple spaces. This avoids
// dedup collisions (the pipeline deduplicates by file+line+rule) while keeping
// edits granular.
func (r *NoMultiSpacesRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	heredocBody := buildHeredocBodyLines(input)

	var violations []rules.Violation

	for i, line := range sm.Lines() {
		// Skip heredoc body lines.
		if heredocBody[i] {
			continue
		}

		// Skip blank lines.
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}

		// Skip comment lines (first non-whitespace is #).
		if trimmed[0] == '#' {
			continue
		}

		indentEnd := len(line) - len(trimmed)
		lineNum := i + 1 // SourceMap is 0-based, locations are 1-based

		edits, firstLoc, totalExtra := scanExtraSpaceRuns(input.File, lineNum, line, indentEnd)
		if len(edits) == 0 {
			continue
		}

		msg := fmt.Sprintf("multiple consecutive spaces (%d extra)", totalExtra)
		v := rules.NewViolation(firstLoc, meta.Code, msg, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithSuggestedFix(&rules.SuggestedFix{
				Description: "Remove extra spaces",
				Safety:      rules.FixSafe,
				Priority:    meta.FixPriority,
				Edits:       edits,
				IsPreferred: true,
			})
		violations = append(violations, v)
	}

	return violations
}

// scanExtraSpaceRuns scans a line for runs of 2+ consecutive spaces after
// indentEnd, skipping content inside single or double quotes. For each run it
// returns a delete-only TextEdit covering the surplus characters (keeping one
// space). firstLoc is the violation location of the first run; totalExtra is
// the total number of excess space characters across all runs.
func scanExtraSpaceRuns(file string, lineNum int, line string, indentEnd int) (
	edits []rules.TextEdit, firstLoc rules.Location, totalExtra int,
) {
	pos := indentEnd
	var inQuote byte // 0 = not in quote, '\'' or '"' = inside that quote

	for pos < len(line) {
		ch := line[pos]

		// Track quote state (simple shell-level quoting).
		if inQuote != 0 {
			if ch == inQuote && (pos == 0 || line[pos-1] != '\\') {
				inQuote = 0
			}
			pos++
			continue
		}
		if ch == '\'' || ch == '"' {
			inQuote = ch
			pos++
			continue
		}

		if ch != ' ' {
			pos++
			continue
		}

		runStart := pos
		for pos < len(line) && line[pos] == ' ' {
			pos++
		}

		runLen := pos - runStart
		if runLen < 2 {
			continue
		}

		// Keep the first space; delete the extras (runStart+1 .. pos).
		violLoc := rules.NewRangeLocation(file, lineNum, runStart, lineNum, pos)
		editLoc := rules.NewRangeLocation(file, lineNum, runStart+1, lineNum, pos)
		if len(edits) == 0 {
			firstLoc = violLoc
		}
		edits = append(edits, rules.TextEdit{
			Location: editLoc,
			NewText:  "",
		})
		totalExtra += runLen - 1
	}

	return edits, firstLoc, totalExtra
}

// buildHeredocBodyLines returns a set of 0-based line indices that are inside
// heredoc bodies. The instruction line itself (e.g. "RUN <<EOF") is NOT marked,
// so it will still be checked for multi-space violations.
func buildHeredocBodyLines(input rules.LintInput) map[int]bool {
	lines := make(map[int]bool)
	if input.AST == nil || input.AST.AST == nil {
		return lines
	}

	sm := input.SourceMap()

	for _, node := range input.AST.AST.Children {
		if len(node.Heredocs) == 0 {
			continue
		}

		// The instruction itself is on node.StartLine (1-based).
		// Heredoc body starts on the next line.
		// endLine covers through the last heredoc terminator.
		endLine := sm.ResolveEndLine(node.EndLine)

		// Mark lines from startLine+1 through endLine as heredoc body (0-based).
		for l := node.StartLine + 1; l <= endLine; l++ {
			lines[l-1] = true // convert 1-based to 0-based
		}
	}

	return lines
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewNoMultiSpacesRule())
}
