// Package shell provides shell script parsing utilities for Dockerfile linting.
package shell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// cmdExit is the shell exit command name, used in multiple checks.
const cmdExit = "exit"

// CountChainedCommands counts the number of commands in && chains within a shell script.
// Pipelines (|) count as a single logical command. Top-level statements separated by
// semicolons or newlines are counted individually.
func CountChainedCommands(script string, variant Variant) int {
	if variant.IsNonPOSIX() {
		return 0
	}

	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return 0
	}

	count := 0
	for _, stmt := range prog.Stmts {
		count += countInStatement(stmt)
	}
	return count
}

// countInStatement counts commands within a single statement, including && chains.
func countInStatement(stmt *syntax.Stmt) int {
	if stmt == nil || stmt.Cmd == nil {
		return 0
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		// Simple command (including pipelines which are represented differently)
		return 1
	case *syntax.BinaryCmd:
		// Only count && chains - || chains have different semantics with set -e
		// and cannot be safely converted to heredocs
		if cmd.Op == syntax.AndStmt {
			return countInStatement(cmd.X) + countInStatement(cmd.Y)
		}
		// || chains and pipes are single logical commands
		return 1
	default:
		// Other compound commands (if, for, while, case, etc.) count as 1
		return 1
	}
}

// ExtractChainedCommands extracts individual command strings from && chains.
// Each command is formatted cleanly using the shell printer.
// Returns nil if parsing fails or for non-POSIX shells.
func ExtractChainedCommands(script string, variant Variant) []string {
	if variant.IsNonPOSIX() {
		return nil
	}

	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil
	}

	var commands []string
	for _, stmt := range prog.Stmts {
		commands = append(commands, extractFromStatement(stmt, variant)...)
	}
	return commands
}

// extractFromStatement extracts commands from a statement, flattening && chains.
// Only && chains are flattened - || chains have different semantics with set -e
// and are kept as single commands (which will fail the IsSimpleScript check).
func extractFromStatement(stmt *syntax.Stmt, variant Variant) []string {
	if stmt == nil || stmt.Cmd == nil {
		return nil
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		if cmd.Op == syntax.AndStmt {
			// Flatten && chains only
			commands := make([]string, 0, 2)
			commands = append(commands, extractFromStatement(cmd.X, variant)...)
			commands = append(commands, extractFromStatement(cmd.Y, variant)...)
			return commands
		}
		// || chains, pipes, or other binary - format as single command
		return []string{FormatStatement(stmt, variant)}
	default:
		return []string{FormatStatement(stmt, variant)}
	}
}

// FormatStatement formats a single statement using syntax.Printer.
// Returns a clean, single-line representation of the command.
func FormatStatement(stmt *syntax.Stmt, variant Variant) string {
	if stmt == nil {
		return ""
	}

	printer := syntax.NewPrinter(
		syntax.Indent(0),
		syntax.SingleLine(true),
	)

	var buf strings.Builder
	if err := printer.Print(&buf, stmt); err != nil {
		return ""
	}
	return strings.TrimSpace(buf.String())
}

// IsSimpleScript checks if a script contains only simple commands that can be
// safely merged into a heredoc. Returns false for scripts with compound commands
// (if, for, while, case), control flow (exit, return), functions, or subshells.
func IsSimpleScript(script string, variant Variant) bool {
	if variant.IsNonPOSIX() {
		return false
	}

	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return false
	}

	for _, stmt := range prog.Stmts {
		if !isSimpleStatement(stmt) {
			return false
		}
	}
	return true
}

// isSimpleStatement checks if a statement is simple enough to merge.
func isSimpleStatement(stmt *syntax.Stmt) bool {
	if stmt == nil || stmt.Cmd == nil {
		return true
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		// Check for control flow commands that break merging semantics
		if len(cmd.Args) > 0 {
			if name := cmd.Args[0].Lit(); name != "" {
				switch name {
				case cmdExit, "return", "break", "continue", "exec":
					return false
				}
			}
		}
		return true
	case *syntax.BinaryCmd:
		// && and || chains are simple if both sides are simple.
		// Since we only split at && (not ||), || chains stay as single lines
		// in the heredoc. Per POSIX, set -e doesn't exit when a command fails
		// as part of a || list, so "cmd || fallback" works correctly with set -e.
		if cmd.Op == syntax.AndStmt || cmd.Op == syntax.OrStmt {
			return isSimpleStatement(cmd.X) && isSimpleStatement(cmd.Y)
		}
		// Pipes are simple
		if cmd.Op == syntax.Pipe || cmd.Op == syntax.PipeAll {
			return true
		}
		return false
	default:
		// IfClause, ForClause, WhileClause, CaseClause, FuncDecl, Block, Subshell, etc.
		return false
	}
}

// IsHeredocCandidate checks if a shell script would be a good candidate for
// heredoc conversion by the prefer-run-heredoc rule. This is used by other rules
// (like DL3003) to avoid generating fixes that would interfere with heredoc conversion.
//
// A script is a heredoc candidate if:
//   - It uses a POSIX shell
//   - It has at least minCommands commands (from && chains or separate statements)
//   - It's a simple script (no complex control flow like if/for/while)
//   - It doesn't contain exit commands
//
// This function parses the script once and reuses the AST for all checks.
func IsHeredocCandidate(script string, variant Variant, minCommands int) bool {
	if variant.IsNonPOSIX() {
		return false
	}

	// Parse once and reuse for all checks
	prog, err := parseScript(script, variant)
	if err != nil {
		return false
	}

	// Must have enough commands to warrant heredoc conversion
	count := countChainedCommandsFromAST(prog)
	if count < minCommands {
		return false
	}

	// Must be simple enough to convert
	if !isSimpleScriptFromAST(prog) {
		return false
	}

	// Exit commands break heredoc merging semantics
	if hasExitCommandFromAST(prog) {
		return false
	}

	return true
}

// parseScript parses a shell script into an AST.
func parseScript(script string, variant Variant) (*syntax.File, error) {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)
	return parser.Parse(strings.NewReader(script), "")
}

// countChainedCommandsFromAST counts commands from a pre-parsed AST.
func countChainedCommandsFromAST(prog *syntax.File) int {
	count := 0
	for _, stmt := range prog.Stmts {
		count += countInStatement(stmt)
	}
	return count
}

// isSimpleScriptFromAST checks if a pre-parsed AST contains only simple commands.
func isSimpleScriptFromAST(prog *syntax.File) bool {
	for _, stmt := range prog.Stmts {
		if !isSimpleStatement(stmt) {
			return false
		}
	}
	return true
}

// hasExitCommandFromAST checks if a pre-parsed AST contains exit commands.
func hasExitCommandFromAST(prog *syntax.File) bool {
	hasExit := false
	syntax.Walk(prog, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok && len(call.Args) > 0 {
			if name := call.Args[0].Lit(); name == cmdExit {
				hasExit = true
				return false // Stop walking
			}
		}
		return true
	})
	return hasExit
}

// HasExitCommand checks if a script contains exit commands that would change
// control flow if merged with other commands.
func HasExitCommand(script string, variant Variant) bool {
	if variant.IsNonPOSIX() {
		return false
	}

	prog, err := parseScript(script, variant)
	if err != nil {
		return false
	}

	return hasExitCommandFromAST(prog)
}
