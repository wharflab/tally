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
// chain boundaries (&& and ||) along with the total command count.
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
	totalCmds := 0

	for _, stmt := range prog.Stmts {
		cmds := collectChainBoundariesFromStmt(stmt, &boundaries)
		totalCmds += cmds
	}

	return boundaries, totalCmds
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
