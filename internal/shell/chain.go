package shell

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ChainPosition describes a command's position within a && chain.
type ChainPosition struct {
	// IsStandalone is true if this is the only command (not chained).
	IsStandalone bool

	// HasOtherStatements is true when the script contains multiple top-level
	// statements separated by semicolons or newlines. In this case,
	// PrecedingCommands and RemainingCommands only cover the chain within
	// the matched statement and do NOT include commands from other statements.
	// Callers building replacement text for the entire script must not use
	// this position alone, as it would silently drop sibling statements.
	HasOtherStatements bool

	// PrecedingCommands contains the commands before this one in the chain.
	// Empty when the command is at the start or standalone.
	PrecedingCommands string

	// RemainingCommands contains the commands after this one in the chain.
	// Empty when the command is at the end or standalone.
	RemainingCommands string
}

// CommandMatcher is a predicate that decides whether a shell call expression
// is the command to locate. name is the base command name (path stripped),
// args are all arguments (flags and positional).
type CommandMatcher func(name string, args []string) bool

// FindCommandInChain locates the first command matching the predicate in a
// shell script and returns its chain context (preceding/remaining commands).
// Returns nil if no matching command is found or the script fails to parse.
func FindCommandInChain(script string, variant Variant, match CommandMatcher) *ChainPosition {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil
	}

	for _, stmt := range prog.Stmts {
		if pos := findInStmt(stmt, script, match); pos != nil {
			// When there are multiple top-level statements separated by ;
			// a single-statement standalone is no longer standalone, and
			// the chain context does not cover sibling statements.
			if len(prog.Stmts) > 1 {
				pos.IsStandalone = false
				pos.HasOtherStatements = true
			}
			return pos
		}
	}

	return nil
}

// findInStmt searches a single statement for a matching command.
func findInStmt(stmt *syntax.Stmt, script string, match CommandMatcher) *ChainPosition {
	if stmt == nil || stmt.Cmd == nil {
		return nil
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		if matchesCall(cmd, match) {
			return &ChainPosition{IsStandalone: true}
		}
	case *syntax.BinaryCmd:
		return findInBinaryCmd(cmd, script, "", "", match)
	}

	return nil
}

// findInBinaryCmd recursively walks a BinaryCmd tree looking for the target.
func findInBinaryCmd(
	bin *syntax.BinaryCmd,
	script string,
	precedingText, followingText string,
	match CommandMatcher,
) *ChainPosition {
	op := binOpText(bin.Op)

	// Build the "following" text for the left side: right side + outer following.
	var leftFollowing string
	if bin.Y != nil {
		rightText := stmtText(bin.Y, script)
		if followingText != "" {
			leftFollowing = rightText + " " + op + " " + followingText
		} else {
			leftFollowing = rightText
		}
	} else {
		leftFollowing = followingText
	}

	// --- Check left side ---
	if bin.X != nil && bin.X.Cmd != nil {
		switch left := bin.X.Cmd.(type) {
		case *syntax.CallExpr:
			if matchesCall(left, match) {
				return &ChainPosition{
					PrecedingCommands: precedingText,
					RemainingCommands: leftFollowing,
				}
			}
		case *syntax.BinaryCmd:
			if pos := findInBinaryCmd(left, script, precedingText, leftFollowing, match); pos != nil {
				return pos
			}
		}
	}

	// Build preceding text for right-side analysis.
	var newPreceding string
	if bin.X != nil {
		leftText := stmtText(bin.X, script)
		if precedingText != "" {
			newPreceding = precedingText + " " + op + " " + leftText
		} else {
			newPreceding = leftText
		}
	}

	// --- Check right side ---
	if bin.Y != nil && bin.Y.Cmd != nil {
		switch right := bin.Y.Cmd.(type) {
		case *syntax.CallExpr:
			if matchesCall(right, match) {
				return &ChainPosition{
					PrecedingCommands: newPreceding,
					RemainingCommands: followingText,
				}
			}
		case *syntax.BinaryCmd:
			if pos := findInBinaryCmd(right, script, newPreceding, followingText, match); pos != nil {
				return pos
			}
		}
	}

	return nil
}

// matchesCall checks whether a CallExpr matches the predicate.
func matchesCall(call *syntax.CallExpr, match CommandMatcher) bool {
	if len(call.Args) == 0 {
		return false
	}
	name := call.Args[0].Lit()
	if name == "" {
		return false
	}
	baseName := path.Base(name)

	var args []string
	for _, arg := range call.Args[1:] {
		if lit := extractQuotedContent(arg); lit != "" {
			args = append(args, lit)
		}
	}
	return match(baseName, args)
}

// binOpText returns the source text representation of a binary command operator.
func binOpText(op syntax.BinCmdOperator) string {
	switch op {
	case syntax.AndStmt:
		return "&&"
	case syntax.OrStmt:
		return "||"
	case syntax.Pipe:
		return "|"
	case syntax.PipeAll:
		return "|&"
	default:
		return "&&"
	}
}

// stmtText extracts the source text for a statement.
func stmtText(stmt *syntax.Stmt, script string) string {
	if stmt == nil {
		return ""
	}
	start := stmt.Pos().Offset()
	end := stmt.End().Offset()
	if start >= uint(len(script)) || end > uint(len(script)) {
		return ""
	}
	return strings.TrimSpace(script[start:end])
}
