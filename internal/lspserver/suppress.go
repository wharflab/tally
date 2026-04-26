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

	// The floor line is the first line after parser directives (# syntax=, # escape=).
	// Comments between parser directives and the first instruction are NOT parser
	// directives — findCommentBlockStart should be free to walk through them.
	parserDirFloor0 := parserDirectiveFloor(parseResult)

	requireReason := cfg != nil && cfg.InlineDirectives.RequireReason
	lines := strings.Split(content, "\n")

	type seenKey struct {
		ruleCode string
		line     int
	}
	seen := make(map[seenKey]struct{})    // dedup per-line actions by (rule, line)
	seenFile := make(map[string]struct{}) // dedup file-level actions by rule only
	var actions []protocol.CodeAction

	for _, v := range violations {
		if !violationMatchesCodeActionParams(v, params) {
			continue
		}

		key := seenKey{ruleCode: v.RuleCode, line: v.Location.Start.Line}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		uri := params.TextDocument.Uri

		// Next-line suppress action
		if edit := suppressLineEdit(v, lines, dirResult, spanIndex, parserDirFloor0, requireReason); edit != nil {
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

		// File-level suppress action (deduplicated by ruleCode only — the same
		// rule on different lines should produce only one "for this file" action).
		if _, fileSeen := seenFile[v.RuleCode]; !fileSeen {
			seenFile[v.RuleCode] = struct{}{}
			if edit := suppressFileEdit(v.RuleCode, lines, dirResult, parserDirFloor0, requireReason); edit != nil {
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
	}

	return actions
}

// parserDirectiveFloor returns the 0-based line after the last parser directive
// in the Dockerfile. Parser directives (# syntax=, # escape=) are consumed by
// BuildKit's parser before building the AST, so they don't appear as children.
// The first child's PrevComment contains comments between parser directives and
// the first instruction. The floor is: firstChild.StartLine - len(PrevComment) - 1.
// This is the lowest line where suppress directives may be inserted.
func parserDirectiveFloor(pr *dockerfile.ParseResult) int {
	if pr == nil || pr.AST == nil || pr.AST.AST == nil {
		return 0
	}
	children := pr.AST.AST.Children
	if len(children) == 0 || children[0] == nil || children[0].StartLine <= 0 {
		return 0
	}

	firstChild := children[0]
	// firstChild.StartLine is 1-based. PrevComment holds the comment lines
	// between parser directives and this instruction. The line right after
	// the last parser directive is:
	//   (firstChild.StartLine - 1) - len(PrevComment)
	// converted to 0-based.
	floor := firstChild.StartLine - 1 - len(firstChild.PrevComment)
	if floor < 0 {
		return 0
	}
	return floor
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
	spanIndex *directive.InstructionSpanIndex,
	parserDirFloor0 int,
	requireReason bool,
) *protocol.TextEdit {
	// Violation line is 1-based; convert to 0-based for line array access.
	violationLine0 := v.Location.Start.Line - 1
	if violationLine0 < 0 || violationLine0 >= len(lines) {
		return nil
	}

	// Resolve to the instruction's start line. A violation may point to a
	// continuation line (e.g. inside a heredoc or after a backslash). The
	// directive must go above the instruction, not in the middle of it.
	instructionLine0 := violationLine0
	if span, ok := spanIndex.ContainingSpan(violationLine0); ok {
		instructionLine0 = span.StartLine
	}

	// Check existing next-line directives that target this instruction.
	// Any source (tally, hadolint, buildx) can suppress the rule, but we
	// only merge into tally-sourced directives to avoid mutating foreign syntax.
	var tallyDirectiveLine *directive.Directive
	for i := range dirResult.Directives {
		d := &dirResult.Directives[i]
		if d.Type != directive.TypeNextLine || !d.AppliesTo.Contains(violationLine0) {
			continue
		}
		if d.SuppressesRule(v.RuleCode) {
			return nil // already suppressed by any directive source
		}
		if d.Source == directive.SourceTally && tallyDirectiveLine == nil {
			tallyDirectiveLine = d
		}
	}
	if tallyDirectiveLine != nil {
		return appendRuleEdit(tallyDirectiveLine.Line, v.RuleCode, lines)
	}

	// No existing directive — insert a new one above the comment block
	// preceding the instruction's start line (not the violation line).
	insertLine0 := findCommentBlockStart(instructionLine0, parserDirFloor0, lines)

	// Match indentation of the instruction start line.
	indent := leadingWhitespace(lines[instructionLine0])
	reason := ""
	if requireReason {
		reason = "TODO"
	}
	comment := indent + directive.FormatNextLine([]string{v.RuleCode}, reason)

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
	parserDirFloor0 int,
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
		return appendRuleEdit(d.Line, ruleCode, lines)
	}

	// No existing global directive — insert right before the first instruction.
	// Everything before parserDirFloor0 is parser directives or preamble comments.
	reason := ""
	if requireReason {
		reason = "TODO"
	}
	comment := directive.FormatGlobal([]string{ruleCode}, reason)

	return &protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: clampUint32(parserDirFloor0), Character: 0},
			End:   protocol.Position{Line: clampUint32(parserDirFloor0), Character: 0},
		},
		NewText: comment + "\n",
	}
}

// appendRuleEdit creates a TextEdit that appends a rule code to an existing
// directive line using directive.AppendRule for the text manipulation.
// directiveLine0 is 0-based.
func appendRuleEdit(directiveLine0 int, ruleCode string, lines []string) *protocol.TextEdit {
	if directiveLine0 < 0 || directiveLine0 >= len(lines) {
		return nil
	}

	edit := directive.AppendRule(lines[directiveLine0], ruleCode)

	return &protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: clampUint32(directiveLine0), Character: clampUint32(edit.Start)},
			End:   protocol.Position{Line: clampUint32(directiveLine0), Character: clampUint32(edit.End)},
		},
		NewText: edit.NewText,
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
