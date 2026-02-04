// Package shell provides shell script parsing utilities for Dockerfile linting.
package shell

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Command names for file creation detection.
const (
	cmdEcho   = "echo"
	cmdCat    = "cat"
	cmdPrintf = "printf"
	cmdChmod  = "chmod"
)

// FileCreationInfo describes a detected file creation pattern in a shell script.
// This is used to coordinate between prefer-copy-heredoc and prefer-run-heredoc rules.
type FileCreationInfo struct {
	// TargetPath is the absolute path to the target file.
	TargetPath string

	// Content is the literal content to write.
	Content string

	// ChmodMode is the octal chmod mode (e.g., "0755", "755"), or empty if no chmod.
	ChmodMode string

	// IsAppend is true if using >> (append) mode for the first write.
	// If true, converting to COPY would lose existing file content.
	IsAppend bool

	// HasUnsafeVariables is true if the script uses variables that cannot be
	// converted to COPY heredoc (e.g., shell variables, command substitution).
	HasUnsafeVariables bool

	// PrecedingCommands contains commands before the file creation (for mixed scripts).
	// Empty if file creation is at the start or script is pure file creation.
	PrecedingCommands string

	// RemainingCommands contains commands after the file creation (for mixed scripts).
	// Empty if file creation is at the end or script is pure file creation.
	RemainingCommands string
}

// fileCreationCmd represents a single file creation command in a chain.
type fileCreationCmd struct {
	targetPath string
	content    string
	isAppend   bool
}

// DetectFileCreation analyzes a shell script for file creation patterns.
// Returns nil if the script is not primarily a file creation operation.
//
// Detected patterns:
//   - echo "content" > /path/to/file
//   - echo "content" >> /path/to/file (append)
//   - cat <<EOF > /path/to/file ... EOF
//   - printf "content" > /path/to/file (limited support)
//
// Also detects chmod chaining: echo "x" > /file && chmod 0755 /file
//
// The knownVars function is called to check if a variable is a known ARG/ENV.
// If nil, all variables are considered unsafe.
func DetectFileCreation(script string, variant Variant, knownVars func(name string) bool) *FileCreationInfo {
	if variant.IsNonPOSIX() {
		return nil
	}

	prog, err := parseScript(script, variant)
	if err != nil {
		return nil
	}

	// Must be a simple script (no complex control flow)
	if !isSimpleScriptFromAST(prog) {
		return nil
	}

	// Analyze the script for file creation patterns
	return analyzeFileCreation(prog, knownVars)
}

// ChmodInfo describes a standalone chmod command.
type ChmodInfo struct {
	// Mode is the octal mode (e.g., "755", "0o644").
	Mode string
	// Target is the file path being chmod'd.
	Target string
}

// DetectStandaloneChmod checks if a shell script is a standalone chmod command.
// Returns nil if it's not a pure chmod or if the chmod cannot be converted
// (e.g., symbolic mode, recursive chmod, multiple commands).
func DetectStandaloneChmod(script string, variant Variant) *ChmodInfo {
	if variant.IsNonPOSIX() {
		return nil
	}

	prog, err := parseScript(script, variant)
	if err != nil {
		return nil
	}

	// Must be exactly one statement
	if len(prog.Stmts) != 1 {
		return nil
	}

	stmt := prog.Stmts[0]
	if stmt.Cmd == nil {
		return nil
	}

	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return nil
	}

	// Must be chmod command
	if call.Args[0].Lit() != cmdChmod {
		return nil
	}

	mode, target := parseChmod(call)
	if mode == "" || target == "" {
		return nil
	}

	return &ChmodInfo{Mode: mode, Target: target}
}

// IsPureFileCreation checks if a shell script is PURELY for creating files.
// Returns true only if every command in the script is for file creation (echo/cat/printf > file)
// or chmod on the created file. Returns false if there are any other commands mixed in.
// This is used by prefer-run-heredoc to yield to prefer-copy-heredoc.
func IsPureFileCreation(script string, variant Variant) bool {
	if variant.IsNonPOSIX() {
		return false
	}

	info := DetectFileCreation(script, variant, nil)
	if info == nil || info.HasUnsafeVariables {
		return false
	}
	// Pure means no other commands before or after
	return info.PrecedingCommands == "" && info.RemainingCommands == ""
}

// cmdType represents the type of command in a chain.
type cmdType int

const (
	cmdTypeOther cmdType = iota
	cmdTypeFileCreation
	cmdTypeChmod
)

// analyzedCmd represents a command with its type and original text.
type analyzedCmd struct {
	cmdType    cmdType
	text       string
	creation   *fileCreationCmd // non-nil for cmdTypeFileCreation
	chmodMode  string           // non-empty for cmdTypeChmod
	chmodTarget string          // non-empty for cmdTypeChmod
	hasUnsafe  bool
}

// analyzeFileCreation performs detailed analysis of file creation patterns.
// Supports mixed commands by tracking preceding and remaining commands.
func analyzeFileCreation(prog *syntax.File, knownVars func(name string) bool) *FileCreationInfo {
	// Require exactly one top-level statement to avoid ambiguity with separators.
	// Scripts with semicolons (cmd1; cmd2) would be incorrectly rebuilt as && chains.
	// Only && chains within a single statement are supported.
	if len(prog.Stmts) != 1 {
		return nil
	}

	// Collect all commands with their types
	var commands []analyzedCmd
	collectCommands(prog, &commands, knownVars)

	if len(commands) == 0 {
		return nil
	}

	// Find contiguous file creation block (including chmod for same file)
	startIdx, endIdx, targetPath := findFileCreationBlock(commands)
	if startIdx == -1 {
		return nil
	}

	// Extract file creation commands and merge content
	var creations []fileCreationCmd
	var chmodMode string
	hasUnsafeVars := false

	for i := startIdx; i <= endIdx; i++ {
		cmd := commands[i]
		if cmd.hasUnsafe {
			hasUnsafeVars = true
		}
		if cmd.cmdType == cmdTypeFileCreation && cmd.creation != nil {
			creations = append(creations, *cmd.creation)
		} else if cmd.cmdType == cmdTypeChmod && cmd.chmodTarget == targetPath {
			chmodMode = cmd.chmodMode
		}
	}

	if len(creations) == 0 {
		return nil
	}

	// Merge content from all creations
	var content strings.Builder
	for i, c := range creations {
		if i > 0 && !c.isAppend {
			content.Reset()
		}
		content.WriteString(c.content)
	}

	// Build preceding commands string
	preceding := make([]string, 0, startIdx)
	for i := range startIdx {
		preceding = append(preceding, commands[i].text)
	}

	// Build remaining commands string
	remainingCount := len(commands) - endIdx - 1
	remaining := make([]string, 0, remainingCount)
	for i := endIdx + 1; i < len(commands); i++ {
		remaining = append(remaining, commands[i].text)
	}

	return &FileCreationInfo{
		TargetPath:         targetPath,
		Content:            content.String(),
		ChmodMode:          chmodMode,
		IsAppend:           creations[0].isAppend,
		HasUnsafeVariables: hasUnsafeVars,
		PrecedingCommands:  strings.Join(preceding, " && "),
		RemainingCommands:  strings.Join(remaining, " && "),
	}
}

// collectCommands flattens && chains and collects all commands with their types.
func collectCommands(prog *syntax.File, commands *[]analyzedCmd, knownVars func(name string) bool) {
	for _, stmt := range prog.Stmts {
		collectFromStatement(stmt, commands, knownVars)
	}
}

// collectFromStatement recursively collects commands from a statement.
func collectFromStatement(stmt *syntax.Stmt, commands *[]analyzedCmd, knownVars func(name string) bool) {
	if stmt == nil || stmt.Cmd == nil {
		return
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		if cmd.Op == syntax.AndStmt {
			collectFromStatement(cmd.X, commands, knownVars)
			collectFromStatement(cmd.Y, commands, knownVars)
		} else {
			// Other binary ops (||, |) - treat as single opaque command
			*commands = append(*commands, analyzedCmd{
				cmdType: cmdTypeOther,
				text:    stmtToString(stmt),
			})
		}
	case *syntax.CallExpr:
		analyzed := analyzeCallExpr(stmt, cmd, knownVars)
		*commands = append(*commands, analyzed)
	default:
		*commands = append(*commands, analyzedCmd{
			cmdType: cmdTypeOther,
			text:    stmtToString(stmt),
		})
	}
}

// analyzeCallExpr analyzes a call expression and returns its type.
func analyzeCallExpr(stmt *syntax.Stmt, call *syntax.CallExpr, knownVars func(name string) bool) analyzedCmd {
	if len(call.Args) == 0 {
		return analyzedCmd{cmdType: cmdTypeOther, text: stmtToString(stmt)}
	}

	cmdName := call.Args[0].Lit()
	text := stmtToString(stmt)

	// Check for chmod
	if cmdName == cmdChmod {
		mode, target := parseChmod(call)
		if mode != "" && target != "" {
			return analyzedCmd{
				cmdType:     cmdTypeChmod,
				text:        text,
				chmodMode:   mode,
				chmodTarget: target,
			}
		}
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	// Check for file creation commands
	if cmdName != cmdEcho && cmdName != cmdCat && cmdName != cmdPrintf {
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	// Validate redirects: allow exactly one stdout output redirect and
	// (for cat) an optional heredoc input.
	var outRedir *syntax.Redirect
	for _, redir := range stmt.Redirs {
		switch redir.Op {
		case syntax.RdrOut, syntax.AppOut:
			if redir.N != nil && redir.N.Value != "1" {
				return analyzedCmd{cmdType: cmdTypeOther, text: text}
			}
			if outRedir != nil {
				return analyzedCmd{cmdType: cmdTypeOther, text: text}
			}
			outRedir = redir
		case syntax.Hdoc, syntax.DashHdoc:
			if cmdName != cmdCat {
				return analyzedCmd{cmdType: cmdTypeOther, text: text}
			}
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.DplOut,
			syntax.ClbOut, syntax.WordHdoc, syntax.RdrAll, syntax.AppAll:
			// Input redirects and other unsupported redirect types
			return analyzedCmd{cmdType: cmdTypeOther, text: text}
		}
	}
	if outRedir == nil {
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	targetPath := extractRedirectTarget(outRedir)
	if targetPath == "" || !path.IsAbs(targetPath) {
		return analyzedCmd{cmdType: cmdTypeOther, text: text}
	}

	content, unsafe := extractFileContent(stmt, call, knownVars)

	return analyzedCmd{
		cmdType: cmdTypeFileCreation,
		text:    text,
		creation: &fileCreationCmd{
			targetPath: targetPath,
			content:    content,
			isAppend:   outRedir.Op == syntax.AppOut,
		},
		hasUnsafe: unsafe,
	}
}

// findFileCreationBlock finds a contiguous block of file creation commands (+ chmod).
// Returns start index, end index, and target path. Returns -1, -1, "" if not found.
func findFileCreationBlock(commands []analyzedCmd) (int, int, string) {
	// Find first file creation
	startIdx := -1
	var targetPath string

	for i, cmd := range commands {
		if cmd.cmdType == cmdTypeFileCreation && cmd.creation != nil {
			startIdx = i
			targetPath = cmd.creation.targetPath
			break
		}
	}

	if startIdx == -1 {
		return -1, -1, ""
	}

	// Extend to include subsequent file creations to same file and chmod
	endIdx := startIdx
	for i := startIdx + 1; i < len(commands); i++ {
		cmd := commands[i]
		switch cmd.cmdType {
		case cmdTypeFileCreation:
			if cmd.creation != nil && cmd.creation.targetPath == targetPath {
				endIdx = i
			} else {
				return startIdx, endIdx, targetPath // Different file, stop
			}
		case cmdTypeChmod:
			if cmd.chmodTarget == targetPath {
				endIdx = i
			} else {
				return startIdx, endIdx, targetPath // Different target, stop
			}
		case cmdTypeOther:
			return startIdx, endIdx, targetPath // Other command, stop
		}
	}

	return startIdx, endIdx, targetPath
}

// stmtToString converts a statement back to string form.
func stmtToString(stmt *syntax.Stmt) string {
	var buf strings.Builder
	printer := syntax.NewPrinter()
	_ = printer.Print(&buf, stmt)
	return strings.TrimSpace(buf.String())
}

// parseChmod extracts mode and target from a chmod command.
// Returns empty strings if the chmod cannot be converted (e.g., symbolic mode, recursive, multiple targets).
func parseChmod(call *syntax.CallExpr) (string, string) {
	if len(call.Args) < 3 {
		return "", ""
	}

	// Skip chmod itself
	args := call.Args[1:]

	var mode, target string
	seenTarget := false

	// Look for mode and target, skipping flags
	for _, arg := range args {
		lit := arg.Lit()
		if lit == "" {
			continue
		}

		// Skip flags (including -R for recursive)
		if strings.HasPrefix(lit, "-") {
			if strings.Contains(lit, "R") {
				// Recursive chmod - skip
				return "", ""
			}
			continue
		}

		// Check if this is an octal mode
		if isOctalMode(lit) {
			mode = lit
			continue
		}

		// Check if this is symbolic mode (e.g., +x, u+rwx)
		if isSymbolicMode(lit) {
			// Convert symbolic to octal (assuming default file mode 0o644)
			converted := symbolicToOctal(lit, defaultFileMode)
			if converted == "" {
				// Unsupported symbolic mode (e.g., +X, +s, +t)
				return "", ""
			}
			mode = converted
			continue
		}

		// Must be a target path
		if mode != "" {
			if seenTarget {
				// Multiple targets (e.g., "chmod 755 /a /b") - not supported
				return "", ""
			}
			target = lit
			seenTarget = true
			continue
		}
	}

	return mode, target
}

// octalModeRegex matches octal chmod modes (3-4 digits).
var octalModeRegex = regexp.MustCompile(`^0?[0-7]{3}$`)

// isOctalMode checks if a string is a valid octal chmod mode.
func isOctalMode(s string) bool {
	return octalModeRegex.MatchString(s)
}

// symbolicModeRegex matches symbolic chmod modes.
var symbolicModeRegex = regexp.MustCompile(`^[ugoa]*[\-+=][rwxXst]+$`)

// defaultFileMode is the typical mode for newly created files (0666 & ~0022 umask).
const defaultFileMode = 0o644

// symbolicToOctal converts a symbolic chmod mode to octal, given a base mode.
// Returns empty string if the mode cannot be converted.
// Supports: [ugoa]*[+-=][rwx]+ (not X, s, t which are complex/rare).
func symbolicToOctal(symbolic string, baseMode int) string {
	if len(symbolic) < 2 {
		return ""
	}

	// Find the operator position
	opIdx := strings.IndexAny(symbolic, "+-=")
	if opIdx == -1 {
		return ""
	}

	who := symbolic[:opIdx]
	op := symbolic[opIdx]
	perms := symbolic[opIdx+1:]

	// Parse who (empty = all)
	var whoMask int
	if who == "" || strings.Contains(who, "a") {
		whoMask = 0o777 // all
	} else {
		if strings.Contains(who, "u") {
			whoMask |= 0o700
		}
		if strings.Contains(who, "g") {
			whoMask |= 0o070
		}
		if strings.Contains(who, "o") {
			whoMask |= 0o007
		}
	}

	// Parse permissions
	var permBits int
	for _, c := range perms {
		switch c {
		case 'r':
			permBits |= 0o444
		case 'w':
			permBits |= 0o222
		case 'x':
			permBits |= 0o111
		case 'X', 's', 't':
			// Not supported - these have complex semantics
			return ""
		}
	}

	// Apply to the who mask
	permBits &= whoMask

	// Apply the operator
	var result int
	switch op {
	case '+':
		result = baseMode | permBits
	case '-':
		result = baseMode &^ permBits
	case '=':
		// Clear the who bits first, then set
		result = (baseMode &^ whoMask) | permBits
	default:
		return ""
	}

	return fmt.Sprintf("%04o", result)
}

// isSymbolicMode checks if a string is a symbolic chmod mode.
func isSymbolicMode(s string) bool {
	return symbolicModeRegex.MatchString(s)
}

// extractRedirectTarget extracts the target path from a redirect.
func extractRedirectTarget(redir *syntax.Redirect) string {
	if redir.Word == nil {
		return ""
	}

	// Only handle literal paths
	return redir.Word.Lit()
}

// extractFileContent extracts the content from a file creation command.
// Returns the content and whether unsafe variables were found.
func extractFileContent(stmt *syntax.Stmt, call *syntax.CallExpr, knownVars func(name string) bool) (string, bool) {
	cmdName := call.Args[0].Lit()

	switch cmdName {
	case cmdEcho:
		return extractEchoContent(call, knownVars)
	case cmdCat:
		// Only heredoc-only cat is safe (e.g., "cat <<EOF > /file")
		// cat with extra args (e.g., "cat /etc/hosts > /file" or "cat -n <<EOF") is unsafe
		// since we can't determine the content at lint time
		if len(call.Args) > 1 {
			return "", true // Mark as unsafe
		}
		return extractCatHeredocContentFromStmt(stmt)
	case cmdPrintf:
		return extractPrintfContent(call, knownVars)
	}

	return "", false
}

// extractCatHeredocContentFromStmt finds and extracts heredoc content from a cat statement.
func extractCatHeredocContentFromStmt(stmt *syntax.Stmt) (string, bool) {
	// Find the heredoc redirect - reject multiple heredocs as ambiguous
	// (bash uses the last input redirect when multiple are present)
	var hdoc *syntax.Redirect
	for _, redir := range stmt.Redirs {
		if redir.Op == syntax.Hdoc || redir.Op == syntax.DashHdoc {
			if hdoc != nil {
				return "", true // multiple heredocs are ambiguous
			}
			hdoc = redir
		}
	}
	if hdoc != nil {
		return extractCatHeredocContent(hdoc)
	}
	// No heredoc found - cat without heredoc creates empty file
	return "", false
}

// extractEchoContent extracts content from an echo command.
func extractEchoContent(call *syntax.CallExpr, knownVars func(name string) bool) (string, bool) {
	if len(call.Args) == 1 {
		// echo with no args prints a newline
		return "\n", false
	}

	// Check for -e flag (escape sequences) - skip for now
	// Check for -n flag (no newline) - handle specially
	hasNoNewline := false
	hasEscape := false
	startIdx := 1

	for i := 1; i < len(call.Args); i++ {
		lit := call.Args[i].Lit()
		// Not an option: no dash prefix or bare "-" (often represents stdin/stdout)
		if !strings.HasPrefix(lit, "-") || lit == "-" {
			startIdx = i
			break
		}
		// "--" ends option parsing
		if lit == "--" {
			startIdx = i + 1
			break
		}
		// Parse known echo options, reject unknown flags
		for _, r := range strings.TrimPrefix(lit, "-") {
			switch r {
			case 'n':
				hasNoNewline = true
			case 'e', 'E':
				hasEscape = true
			default:
				// Unknown option letter (e.g., -x) - mark unsafe
				return "", true
			}
		}
		startIdx = i + 1
	}

	// Skip -e for now (complex escape handling)
	if hasEscape {
		return "", true
	}

	// echo -n produces no trailing newline; COPY heredoc can't represent that
	if hasNoNewline {
		return "", true
	}

	var content strings.Builder
	hasUnsafe := false

	for i := startIdx; i < len(call.Args); i++ {
		if i > startIdx {
			content.WriteString(" ")
		}

		argContent, unsafe := extractWordContent(call.Args[i], knownVars)
		if unsafe {
			hasUnsafe = true
		}
		content.WriteString(argContent)
	}

	result := content.String() + "\n"

	return result, hasUnsafe
}

// isComplexExpansion checks if a ParamExp uses complex expansion features
// (e.g., ${#VAR}, ${VAR:-default}, ${VAR:0:5}, etc.)
func isComplexExpansion(p *syntax.ParamExp) bool {
	return p.Excl || p.Length || p.Width || p.Index != nil ||
		p.Slice != nil || p.Repl != nil || p.Exp != nil
}

// extractParamExpContent handles parameter expansion, writing to content and returning unsafe status.
func extractParamExpContent(p *syntax.ParamExp, content *strings.Builder, knownVars func(name string) bool) bool {
	varName := p.Param.Value
	if knownVars != nil && knownVars(varName) {
		// Known ARG/ENV - can be preserved in COPY heredoc
		// Always brace to avoid $VARsuffix ambiguity
		content.WriteString("${")
		content.WriteString(varName)
		content.WriteString("}")
		return isComplexExpansion(p)
	}
	content.WriteString("$")
	content.WriteString(varName)
	return true
}

// extractWordContent extracts the literal content from a word.
func extractWordContent(word *syntax.Word, knownVars func(name string) bool) (string, bool) {
	var content strings.Builder
	hasUnsafe := false

	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			content.WriteString(p.Value)
		case *syntax.SglQuoted:
			content.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dpart := range p.Parts {
				switch dp := dpart.(type) {
				case *syntax.Lit:
					content.WriteString(dp.Value)
				case *syntax.ParamExp:
					if extractParamExpContent(dp, &content, knownVars) {
						hasUnsafe = true
					}
				default:
					// Command substitution, arithmetic, etc.
					hasUnsafe = true
				}
			}
		case *syntax.ParamExp:
			if extractParamExpContent(p, &content, knownVars) {
				hasUnsafe = true
			}
		default:
			// Command substitution, arithmetic, etc.
			hasUnsafe = true
		}
	}

	return content.String(), hasUnsafe
}

// extractCatHeredocContent extracts content from a cat heredoc.
func extractCatHeredocContent(redir *syntax.Redirect) (string, bool) {
	if redir.Hdoc == nil {
		return "", false
	}

	// Get heredoc content
	var content strings.Builder
	for _, part := range redir.Hdoc.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			content.WriteString(p.Value)
		default:
			// Variable expansion in heredoc - mark as potentially unsafe
			return content.String(), true
		}
	}

	result := content.String()
	// <<- (DashHdoc) strips leading tabs from each line
	if redir.Op == syntax.DashHdoc {
		lines := strings.Split(result, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimLeft(line, "\t")
		}
		result = strings.Join(lines, "\n")
	}
	return result, false
}

// extractPrintfContent extracts content from a printf command.
// Limited support - only handles simple "%s" or literal format strings.
func extractPrintfContent(call *syntax.CallExpr, knownVars func(name string) bool) (string, bool) {
	if len(call.Args) < 2 {
		return "", false
	}

	// Get format string
	format := call.Args[1].Lit()
	if format == "" {
		return "", true // Complex format - unsafe
	}

	// Very limited: only handle literal strings without format specifiers
	if strings.Contains(format, "%") && format != "%s" {
		return "", true
	}

	if format == "%s" && len(call.Args) >= 3 {
		if len(call.Args) != 3 {
			return "", true // extra args repeat format; unsafe
		}
		// Simple %s with argument
		content, unsafe := extractWordContent(call.Args[2], knownVars)
		// printf doesn't add trailing newline; mark unsafe unless content has one
		if unsafe || !strings.HasSuffix(content, "\n") {
			return "", true
		}
		return content, false
	}

	// Literal string (escape sequences would need processing)
	if strings.ContainsAny(format, "\\") {
		return "", true // Has escape sequences - complex
	}
	if len(call.Args) != 2 {
		return "", true // extra args repeat format; unsafe
	}

	// printf doesn't add trailing newline; mark unsafe unless content has one
	if !strings.HasSuffix(format, "\n") {
		return "", true
	}

	return format, false
}

// NormalizeOctalMode normalizes a chmod mode to 4-digit octal format.
// E.g., "755" -> "0755", "0755" -> "0755"
// Other inputs (empty, 2-digit, 5+ digit) are returned unchanged.
func NormalizeOctalMode(mode string) string {
	if len(mode) == 3 {
		return "0" + mode
	}
	return mode
}
