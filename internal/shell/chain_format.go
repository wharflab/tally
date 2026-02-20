package shell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ChainBoundary represents the position of a chain operator (&&/||)
// between two commands in a shell script's source text.
type ChainBoundary struct {
	// LeftEndLine is the 1-based line (in the parsed text) where the left command ends.
	LeftEndLine int
	// LeftEndCol is the 1-based column where the left command ends.
	LeftEndCol int
	// RightStartLine is the 1-based line where the right command starts.
	RightStartLine int
	// RightStartCol is the 1-based column where the right command starts.
	RightStartCol int
	// Op is the operator text ("&&" or "||").
	Op string
	// SameLine is true when left end and right start are on the same source line.
	SameLine bool
}

// CollectChainBoundaries parses a shell script and returns all top-level
// chain boundaries (&& and ||) along with the maximum per-chain command count.
// The per-chain count reflects the longest &&/|| chain in any single statement,
// not the sum across semicolon-separated statements. This is the correct value
// for comparing against a minCommands threshold.
//
// The script text should include backslash continuations exactly as they
// appear in the Dockerfile source so that line/col positions map correctly.
//
// Returns nil, 0 if parsing fails or for non-POSIX shells.
func CollectChainBoundaries(scriptText string, variant Variant) ([]ChainBoundary, int) {
	if variant.IsNonPOSIX() {
		return nil, 0
	}

	prog, err := parseScript(scriptText, variant)
	if err != nil {
		return nil, 0
	}

	var boundaries []ChainBoundary
	maxChainCmds := 0

	for _, stmt := range prog.Stmts {
		cmds := collectChainBoundariesFromStmt(stmt, &boundaries)
		if cmds > maxChainCmds {
			maxChainCmds = cmds
		}
	}

	return boundaries, maxChainCmds
}

// collectChainBoundariesFromStmt recursively collects chain boundaries from a statement.
// Returns the number of leaf commands in this statement.
func collectChainBoundariesFromStmt(stmt *syntax.Stmt, boundaries *[]ChainBoundary) int {
	if stmt == nil || stmt.Cmd == nil {
		return 0
	}

	bin, ok := stmt.Cmd.(*syntax.BinaryCmd)
	if !ok {
		return 1 // leaf command
	}

	if bin.Op != syntax.AndStmt && bin.Op != syntax.OrStmt {
		return 1 // pipe or other — treat as single command
	}

	leftCount := collectChainBoundariesFromStmt(bin.X, boundaries)

	leftEnd := bin.X.End()
	rightStart := bin.Y.Pos()
	op := binOpText(bin.Op)

	//nolint:gosec // syntax.Pos line/col values always fit int
	b := ChainBoundary{
		LeftEndLine:    int(leftEnd.Line()),
		LeftEndCol:     int(leftEnd.Col()),
		RightStartLine: int(rightStart.Line()),
		RightStartCol:  int(rightStart.Col()),
		Op:             op,
		SameLine:       leftEnd.Line() == rightStart.Line(),
	}
	*boundaries = append(*boundaries, b)

	rightCount := collectChainBoundariesFromStmt(bin.Y, boundaries)

	return leftCount + rightCount
}

// ScriptHasInlineHeredoc checks whether a shell script contains inline heredocs
// (e.g., cat <<EOF ... EOF && other_cmd). Such scripts should not have their
// chain boundaries reformatted because the heredoc body positions would break.
func ScriptHasInlineHeredoc(script string, variant Variant) bool {
	if variant.IsNonPOSIX() {
		return false
	}

	prog, err := parseScript(script, variant)
	if err != nil {
		return false
	}

	found := false
	syntax.Walk(prog, func(node syntax.Node) bool {
		if found {
			return false
		}
		if redirect, ok := node.(*syntax.Redirect); ok {
			if redirect.Hdoc != nil {
				found = true
				return false
			}
		}
		return true
	})

	return found
}

// FormatChainedScript formats a shell script so that each top-level &&/||
// chain operator starts on its own line. Uses the mvdan.cc/sh/v3 printer with
// BinaryNextLine for correct shell formatting. Returns the original script text
// (trimmed) if parsing fails or there are no chain operators.
//
// The output uses tab indentation for continuation lines (Indent(0) = tabs).
func FormatChainedScript(script string, variant Variant) string {
	trimmed := strings.TrimSpace(script)
	if variant.IsNonPOSIX() {
		return trimmed
	}

	prog, err := parseScript(script, variant)
	if err != nil {
		return trimmed
	}

	if !forceChainNewlines(prog) {
		return trimmed
	}

	var buf strings.Builder
	printer := syntax.NewPrinter(syntax.BinaryNextLine(true), syntax.Indent(0))
	if err := printer.Print(&buf, prog); err != nil {
		return trimmed
	}
	return strings.TrimSpace(buf.String())
}

// forceChainNewlines modifies the AST so that every top-level &&/|| BinaryCmd
// has its right-hand side on a subsequent line. This causes the printer to emit
// multi-line output with BinaryNextLine(true). Returns true if any positions
// were modified.
func forceChainNewlines(prog *syntax.File) bool {
	modified := false
	line := uint(1)
	for _, stmt := range prog.Stmts {
		if stmt.Cmd != nil {
			if forceChainNewlinesRec(stmt.Cmd, &line) {
				modified = true
			}
		}
	}
	return modified
}

// forceChainNewlinesRec recursively assigns increasing line numbers to the
// right-hand side of &&/|| BinaryCmd nodes (bottom-up).
func forceChainNewlinesRec(cmd syntax.Command, nextLine *uint) bool {
	bin, ok := cmd.(*syntax.BinaryCmd)
	if !ok || (bin.Op != syntax.AndStmt && bin.Op != syntax.OrStmt) {
		return false
	}

	// Process left subtree first (bottom-up for nested left-associative chains).
	if bin.X.Cmd != nil {
		forceChainNewlinesRec(bin.X.Cmd, nextLine)
	}

	// Force this boundary's right-hand side onto the next line.
	*nextLine++
	bin.OpPos = syntax.NewPos(0, *nextLine, 1)
	bin.Y.Position = syntax.NewPos(0, *nextLine, 1)

	// Process right subtree (for patterns like (a && b) || (c && d)).
	if bin.Y.Cmd != nil {
		forceChainNewlinesRec(bin.Y.Cmd, nextLine)
	}

	return true
}

// ReconstructSourceText reconstructs the shell command source text from
// Dockerfile source lines. Backslash-newline continuations are kept intact
// because the shell parser (mvdan.cc/sh) handles them natively. This
// preserves line/col positions for mapping back to the Dockerfile.
//
// cmdStartCol is the byte offset in the first line where the command starts
// (after RUN + flags). Continuation lines are included in full.
func ReconstructSourceText(lines []string, cmdStartCol int) string {
	var sb strings.Builder
	for i, line := range lines {
		if i == 0 {
			// First line: take only the command portion (after RUN + flags)
			if cmdStartCol < len(line) {
				line = line[cmdStartCol:]
			} else {
				line = ""
			}
		}

		sb.WriteString(line)
		if i < len(lines)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
