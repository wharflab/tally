package lspserver

import (
	"fmt"
	"strings"

	protocol "github.com/wharflab/tally/internal/lsp/protocol"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/sourcemap"
)

const suppressCodeActionKind = protocol.CodeActionKind("quickfix.suppress.tally")

// suppressRuleActions generates CodeActions that inject # tally ignore= directives
// to suppress violations. For each violation in the requested range, up to two
// actions are emitted: "Suppress {ruleCode} for this line" (next-line directive)
// and "Suppress {ruleCode} for this file" (global directive).
func suppressRuleActions(
	violations []rules.Violation,
	params *protocol.CodeActionParams,
	content string,
	parseResult *dockerfile.ParseResult,
	cfg *config.Config,
) []protocol.CodeAction {
	if parseResult == nil || parseResult.AST == nil {
		return nil
	}

	sm := sourcemap.New(parseResult.Source)
	spanIndex := directive.NewInstructionSpanIndexFromAST(parseResult.AST, sm)
	dirResult := directive.Parse(sm, nil, spanIndex)

	// The first instruction's start line (0-based) marks where parser directives
	// end. Derived from the already-parsed AST — no string-based parser directive
	// detection needed.
	firstInstLine0 := firstInstructionLine(parseResult)

	requireReason := cfg != nil && cfg.InlineDirectives.RequireReason
	lines := strings.Split(content, "\n")

	type seenKey struct {
		ruleCode string
		line     int
	}
	seen := make(map[seenKey]struct{})
	var actions []protocol.CodeAction

	for _, v := range violations {
		vRange := violationRange(v)
		if !rangesOverlap(vRange, params.Range) {
			continue
		}

		key := seenKey{ruleCode: v.RuleCode, line: v.Location.Start.Line}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		uri := params.TextDocument.Uri

		// Next-line suppress action
		if edit := suppressLineEdit(v, lines, dirResult, firstInstLine0, requireReason); edit != nil {
			suppressLineData := any(map[string]any{"type": "suppress-line", "ruleCode": v.RuleCode})
			actions = append(actions, protocol.CodeAction{
				Title: fmt.Sprintf("Suppress %s for this line", v.RuleCode),
				Kind:  ptrTo(suppressCodeActionKind),
				Edit: &protocol.WorkspaceEdit{
					Changes: new(map[protocol.DocumentUri][]*protocol.TextEdit{
						uri: {edit},
					}),
				},
				Data: &suppressLineData,
			})
		}

		// File-level suppress action
		if edit := suppressFileEdit(v.RuleCode, lines, dirResult, firstInstLine0, requireReason); edit != nil {
			suppressFileData := any(map[string]any{"type": "suppress-file", "ruleCode": v.RuleCode})
			actions = append(actions, protocol.CodeAction{
				Title: fmt.Sprintf("Suppress %s for this file", v.RuleCode),
				Kind:  ptrTo(suppressCodeActionKind),
				Edit: &protocol.WorkspaceEdit{
					Changes: new(map[protocol.DocumentUri][]*protocol.TextEdit{
						uri: {edit},
					}),
				},
				Data: &suppressFileData,
			})
		}
	}

	return actions
}

// firstInstructionLine returns the 0-based line of the first instruction in the
// Dockerfile, derived from the already-parsed AST. Everything before this line
// is parser directives or comments. Returns 0 if there are no instructions.
func firstInstructionLine(pr *dockerfile.ParseResult) int {
	if pr == nil || pr.AST == nil || pr.AST.AST == nil {
		return 0
	}
	for _, child := range pr.AST.AST.Children {
		if child != nil && child.StartLine > 0 {
			return child.StartLine - 1 // AST uses 1-based lines
		}
	}
	return 0
}

// suppressLineEdit generates a TextEdit to add a # tally ignore= directive
// for the given violation. It inserts above the comment block preceding the
// instruction (not between comments and the instruction) to avoid triggering
// buildkit/InvalidDefinitionDescription.
//
// If an existing next-line directive already covers this instruction, the rule
// code is appended to it instead.
func suppressLineEdit(
	v rules.Violation,
	lines []string,
	dirResult *directive.ParseResult,
	firstInstLine0 int,
	requireReason bool,
) *protocol.TextEdit {
	// Violation line is 1-based; convert to 0-based for line array access.
	violationLine0 := v.Location.Start.Line - 1
	if violationLine0 < 0 || violationLine0 >= len(lines) {
		return nil
	}

	// Check if an existing next-line directive already targets this instruction.
	for i := range dirResult.Directives {
		d := &dirResult.Directives[i]
		if d.Type != directive.TypeNextLine {
			continue
		}
		if !d.AppliesTo.Contains(violationLine0) {
			continue
		}
		// Found an existing directive — check if it already suppresses this rule.
		if d.SuppressesRule(v.RuleCode) {
			return nil // already suppressed
		}
		// Merge: append the rule code to the existing directive line.
		return mergeRuleIntoDirectiveLine(d.Line, v.RuleCode, lines)
	}

	// No existing directive — insert a new one above the comment block,
	// but never before the first instruction (which marks the end of parser directives).
	insertLine0 := findCommentBlockStart(violationLine0, firstInstLine0, lines)

	// Match indentation of the instruction line.
	indent := leadingWhitespace(lines[violationLine0])
	comment := indent + "# tally ignore=" + v.RuleCode
	if requireReason {
		comment += ";reason=TODO"
	}

	return &protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: clampUint32(insertLine0), Character: 0},
			End:   protocol.Position{Line: clampUint32(insertLine0), Character: 0},
		},
		NewText: comment + "\n",
	}
}

// suppressFileEdit generates a TextEdit to add a # tally global ignore= directive
// at the top of the file. If an existing global directive exists, the rule code
// is appended to it.
func suppressFileEdit(
	ruleCode string,
	lines []string,
	dirResult *directive.ParseResult,
	firstInstLine0 int,
	requireReason bool,
) *protocol.TextEdit {
	// Check existing global directives.
	for i := range dirResult.Directives {
		d := &dirResult.Directives[i]
		if d.Type != directive.TypeGlobal {
			continue
		}
		if d.Source != directive.SourceTally {
			continue
		}
		if d.SuppressesRule(ruleCode) {
			return nil // already suppressed
		}
		return mergeRuleIntoDirectiveLine(d.Line, ruleCode, lines)
	}

	// No existing global directive — insert right before the first instruction.
	// Everything before firstInstLine0 is parser directives or preamble comments.
	comment := "# tally global ignore=" + ruleCode
	if requireReason {
		comment += ";reason=TODO"
	}

	return &protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: clampUint32(firstInstLine0), Character: 0},
			End:   protocol.Position{Line: clampUint32(firstInstLine0), Character: 0},
		},
		NewText: comment + "\n",
	}
}

// mergeRuleIntoDirectiveLine appends a rule code to an existing directive line.
// directiveLine is 0-based.
func mergeRuleIntoDirectiveLine(directiveLine int, ruleCode string, lines []string) *protocol.TextEdit {
	if directiveLine < 0 || directiveLine >= len(lines) {
		return nil
	}

	line := lines[directiveLine]

	// Find the position to insert: before ;reason= if present, otherwise at end of line.
	insertPos := len(line)
	if idx := strings.Index(line, ";reason="); idx >= 0 {
		insertPos = idx
	}

	// Trim trailing whitespace before insertion point.
	trimmed := insertPos
	for trimmed > 0 && line[trimmed-1] == ' ' {
		trimmed--
	}

	return &protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: clampUint32(directiveLine), Character: clampUint32(trimmed)},
			End:   protocol.Position{Line: clampUint32(directiveLine), Character: clampUint32(insertPos)},
		},
		NewText: "," + ruleCode,
	}
}

// findCommentBlockStart walks backwards from the instruction line to find
// the first line of any comment block immediately above it. Returns the
// 0-based line where the new directive should be inserted.
//
// floorLine0 is the lowest line we may return (typically the first instruction
// line from the AST), preventing insertion into the parser-directive preamble.
//
// Stops at:
//   - empty lines or non-comment lines (obvious block boundary)
//   - bare "#" lines (empty comment — acts as a block separator in BuildKit)
//   - floorLine0 (never walks into parser directives)
func findCommentBlockStart(instructionLine0, floorLine0 int, lines []string) int {
	line := instructionLine0
	for line > floorLine0 {
		prev := strings.TrimSpace(lines[line-1])
		if prev == "" || !strings.HasPrefix(prev, "#") {
			break
		}
		// Bare "#" (empty comment) is a block separator.
		if prev == "#" {
			break
		}
		line--
	}
	return line
}

// leadingWhitespace returns the leading whitespace (tabs and spaces) of a line.
func leadingWhitespace(line string) string {
	for i, c := range line {
		if c != ' ' && c != '\t' {
			return line[:i]
		}
	}
	return line
}
