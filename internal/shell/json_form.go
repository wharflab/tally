package shell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// LooksLikeJSONExecForm reports whether a shell script fragment appears to be
// a Docker JSON exec-form attempt that was not valid JSON. It uses the
// active shell variant's parser to distinguish valid shell syntax from
// likely exec-form mistakes.
//
// For POSIX shells (bash, sh, etc.), the script is parsed with mvdan.cc/sh
// and the AST is inspected: a top-level CallExpr whose first word starts with
// [ or last word ends with ] signals a likely JSON attempt, while proper test
// constructs (TestClause, BinaryCmd) are recognized as valid shell.
//
// For PowerShell, the tree-sitter grammar is used: if the script parses without
// errors, it is valid PowerShell (e.g., [ClassName]::Method) and not JSON.
func LooksLikeJSONExecForm(script string, variant Variant) bool {
	trimmed := strings.TrimSpace(script)
	if trimmed == "" {
		return false
	}

	// Quick structural pre-check: must start with [ or end with ] to
	// be a candidate for JSON exec-form.
	if !strings.HasPrefix(trimmed, "[") && !strings.HasSuffix(trimmed, "]") {
		return false
	}

	// Bash [[ test syntax is never a JSON array attempt.
	if strings.HasPrefix(trimmed, "[[") {
		return false
	}

	if variant.SupportsPOSIXShellAST() {
		return posixLooksLikeJSON(trimmed, variant)
	}
	if variant.IsPowerShell() {
		return powerShellLooksLikeJSON(trimmed)
	}

	// For cmd, unknown variants, or no parser: the pre-check already
	// confirmed bracket signals — treat as a possible JSON attempt.
	return true
}

// posixLooksLikeJSON uses the mvdan.cc/sh parser to determine if the script
// looks like a JSON exec-form attempt in a POSIX-compatible shell.
//
// Key insight: the bash parser produces distinct AST nodes for shell test
// constructs ([[ ]] → TestClause, [ ] && cmd → BinaryCmd) vs plain command
// invocations (CallExpr). A CallExpr whose first word starts with [ (but is
// not the bare [ test builtin) or whose last word ends with ] (but is not the
// bare ] closing of a test) is a strong signal of a JSON exec-form attempt.
func posixLooksLikeJSON(script string, variant Variant) bool {
	prog, err := parseScript(script, variant)
	if err != nil {
		// Parse failed entirely — if it has JSON-like brackets, treat as JSON.
		return true
	}

	if len(prog.Stmts) == 0 {
		return false
	}

	// Only a top-level CallExpr can be a JSON form attempt.
	// BinaryCmd ([ -f file ] && echo), TestClause ([[ ... ]]), pipes, etc.
	// are recognized shell constructs — not JSON.
	call, ok := prog.Stmts[0].Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 {
		return false
	}

	if firstWordStartsWithJSONBracket(call.Args[0]) {
		return true
	}

	if lastWordEndsWithJSONBracket(call.Args[len(call.Args)-1]) {
		return true
	}

	return false
}

// firstWordStartsWithJSONBracket checks if the first word of a CallExpr
// starts with [ in a way that suggests JSON exec-form rather than the
// POSIX test builtin [.
//
// Returns true for:  [bash,  ["echo",  [/bin/sh,
// Returns false for: [  (bare test builtin), echo
func firstWordStartsWithJSONBracket(word *syntax.Word) bool {
	if len(word.Parts) == 0 {
		return false
	}
	lit, ok := word.Parts[0].(*syntax.Lit)
	if !ok {
		return false
	}
	if lit.Value == "[" {
		// Bare [ is the test builtin — unless followed by more word parts
		// (e.g., ["echo", where [ is Lit and "echo" is DblQuoted).
		return len(word.Parts) > 1
	}
	return strings.HasPrefix(lit.Value, "[")
}

// lastWordEndsWithJSONBracket checks if the last word of a CallExpr
// ends with ] in a way that suggests JSON exec-form closing rather than
// the POSIX test builtin's closing ].
//
// Returns true for:  "echo hello"]  -c]  ,]
// Returns false for: ]  (bare test closing), hello
func lastWordEndsWithJSONBracket(word *syntax.Word) bool {
	if len(word.Parts) == 0 {
		return false
	}
	lastPart := word.Parts[len(word.Parts)-1]
	lit, ok := lastPart.(*syntax.Lit)
	if !ok {
		return false
	}
	if lit.Value == "]" {
		// Bare ] is the test builtin closing — unless preceded by more parts
		// (e.g., "echo"] where "echo" is DblQuoted and ] is Lit).
		return len(word.Parts) > 1
	}
	return strings.HasSuffix(lit.Value, "]")
}

// powerShellLooksLikeJSON checks if a script looks like JSON exec-form
// when the active shell is PowerShell. If the PowerShell parser can
// parse the script without errors, it is valid PowerShell (not JSON).
func powerShellLooksLikeJSON(script string) bool {
	// Normalize Dockerfile \ line continuations which are not native to
	// PowerShell (PowerShell uses ` for line continuation).
	normalized := strings.ReplaceAll(script, "\\\n", " ")
	if canParsePowerShell(strings.TrimSpace(normalized)) {
		return false
	}
	// PowerShell parser failed or unavailable — might be JSON.
	return true
}
