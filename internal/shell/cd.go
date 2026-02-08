package shell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CdCommand represents a cd command found in a shell script.
type CdCommand struct {
	// TargetDir is the directory argument passed to cd.
	TargetDir string

	// IsStandalone is true if cd is the only command (not chained with && or ;).
	IsStandalone bool

	// IsAtStart is true if cd is at the beginning of a command chain.
	// e.g., "cd /foo && make" has cd at start, "make && cd /foo" does not.
	IsAtStart bool

	// PrecedingCommands contains the commands before cd if it's not at the start.
	// e.g., for "mkdir /tmp && cd /tmp && make", this would be "mkdir /tmp".
	PrecedingCommands string

	// RemainingCommands contains the commands after "cd /foo &&" if IsAtStart is true.
	// Empty if IsStandalone is true or cd is not at start.
	RemainingCommands string

	// StartCol is the 0-based column where cd starts.
	StartCol int

	// Line is the 0-based line number.
	Line int
}

// FindCdCommands finds all cd commands in a shell script and analyzes their context.
func FindCdCommands(script string, variant Variant) []CdCommand {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil
	}

	var results []CdCommand

	// Process each statement at the top level
	for i, stmt := range prog.Stmts {
		cmds := analyzeCdInStatement(stmt, script)
		// When there are multiple top-level statements (e.g., "cd /app; make"),
		// a cd is not standalone and only the first statement is "at start"
		if len(prog.Stmts) > 1 {
			for j := range cmds {
				cmds[j].IsStandalone = false
				if i > 0 {
					cmds[j].IsAtStart = false
				}
			}
		}
		results = append(results, cmds...)
	}

	return results
}

// analyzeCdInStatement analyzes a statement for cd commands.
func analyzeCdInStatement(stmt *syntax.Stmt, script string) []CdCommand {
	if stmt == nil || stmt.Cmd == nil {
		return nil
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		// Simple command - check if it's cd
		if isCdCall(cmd) {
			cd := extractCdInfo(cmd)
			cd.IsStandalone = true
			cd.IsAtStart = true
			return []CdCommand{cd}
		}

	case *syntax.BinaryCmd:
		// Command chain - analyze for cd position
		return analyzeBinaryCmd(cmd, script, true)
	}

	return nil
}

// analyzeBinaryCmd recursively analyzes a binary command for cd.
// precedingText accumulates commands seen to the left of the current binary expression.
func analyzeBinaryCmd(bin *syntax.BinaryCmd, script string, isLeftmost bool) []CdCommand {
	return analyzeBinaryCmdWithContext(bin, script, isLeftmost, "", "")
}

// analyzeBinaryCmdWithContext is the internal recursive function that tracks context.
// precedingText: commands accumulated to the left
// followingText: commands that come after (from outer context)
func analyzeBinaryCmdWithContext(bin *syntax.BinaryCmd, script string, isLeftmost bool, precedingText, followingText string) []CdCommand {
	var results []CdCommand

	// Calculate what comes after the left side (bin.Y plus outer following)
	var leftFollowing string
	if bin.Y != nil {
		rightText := extractRemainingScript(bin.Y, script)
		if followingText != "" {
			leftFollowing = rightText + " && " + followingText
		} else {
			leftFollowing = rightText
		}
	} else {
		leftFollowing = followingText
	}

	// Check left side
	if bin.X != nil && bin.X.Cmd != nil {
		switch left := bin.X.Cmd.(type) {
		case *syntax.CallExpr:
			if isCdCall(left) {
				cd := extractCdInfo(left)
				cd.IsStandalone = false
				cd.IsAtStart = isLeftmost && precedingText == ""

				// Preceding commands from outer context
				cd.PrecedingCommands = precedingText

				// Everything to the right (bin.Y) plus outer following
				cd.RemainingCommands = leftFollowing
				results = append(results, cd)
			}

		case *syntax.BinaryCmd:
			// Nested binary command on left - recurse, passing context
			results = append(results, analyzeBinaryCmdWithContext(left, script, isLeftmost, precedingText, leftFollowing)...)
		}
	}

	// Build the new preceding text for right-side analysis
	var newPrecedingText string
	if bin.X != nil {
		leftText := extractRemainingScript(bin.X, script)
		if precedingText != "" {
			newPrecedingText = precedingText + " && " + leftText
		} else {
			newPrecedingText = leftText
		}
	}

	// Check right side
	if bin.Y != nil && bin.Y.Cmd != nil {
		switch right := bin.Y.Cmd.(type) {
		case *syntax.CallExpr:
			if isCdCall(right) {
				cd := extractCdInfo(right)
				cd.IsStandalone = false
				cd.IsAtStart = false // Right side is never at start

				// Set preceding commands
				cd.PrecedingCommands = newPrecedingText
				// Following comes from outer context
				cd.RemainingCommands = followingText
				results = append(results, cd)
			}

		case *syntax.BinaryCmd:
			// Nested binary command on right - recurse with context
			results = append(results, analyzeBinaryCmdWithContext(right, script, false, newPrecedingText, followingText)...)
		}
	}

	return results
}

// extractRemainingScript extracts the script text for a statement and its children.
func extractRemainingScript(stmt *syntax.Stmt, script string) string {
	if stmt == nil {
		return ""
	}

	// Get the full range of this statement
	start := stmt.Pos().Offset()
	end := stmt.End().Offset()

	if start >= uint(len(script)) || end > uint(len(script)) {
		return ""
	}

	return strings.TrimSpace(script[start:end])
}

// isCdCall checks if a CallExpr is a cd command.
func isCdCall(call *syntax.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	name := call.Args[0].Lit()
	return name == "cd"
}

// extractCdInfo extracts information about a cd command.
func extractCdInfo(call *syntax.CallExpr) CdCommand {
	cd := CdCommand{
		StartCol: int(call.Pos().Col()) - 1,  //nolint:gosec // safe: column numbers are small
		Line:     int(call.Pos().Line()) - 1, //nolint:gosec // safe: line numbers are small
	}

	// Extract target directory (second argument)
	if len(call.Args) > 1 {
		cd.TargetDir = extractQuotedContent(call.Args[1])
	}

	return cd
}

// HasStandaloneCd returns true if the script contains a standalone cd command
// (one that isn't chained with other commands).
func HasStandaloneCd(script string, variant Variant) bool {
	for _, cd := range FindCdCommands(script, variant) {
		if cd.IsStandalone {
			return true
		}
	}
	return false
}

// HasCdAtStart returns true if the script has cd at the beginning of a command chain.
func HasCdAtStart(script string, variant Variant) bool {
	for _, cd := range FindCdCommands(script, variant) {
		if cd.IsAtStart {
			return true
		}
	}
	return false
}

// ExtractCommandsBetweenCds parses the remaining commands after a cd and extracts
// commands that come before the next cd. This properly handles quoted paths.
// For "make && cd /tmp && build", if we're looking for commands before "cd /tmp",
// this returns "make".
func ExtractCommandsBetweenCds(remaining string, variant Variant) string {
	if remaining == "" {
		return ""
	}

	// Parse remaining to find cd commands
	cds := FindCdCommands(remaining, variant)
	if len(cds) == 0 {
		// No cd found - return everything
		return remaining
	}

	// Get the first cd in remaining
	cd := cds[0]

	// If cd is at the start, there's nothing before it
	if cd.IsAtStart || cd.PrecedingCommands == "" {
		return ""
	}

	return cd.PrecedingCommands
}
