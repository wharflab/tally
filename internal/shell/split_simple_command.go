package shell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// SplitSimpleCommand parses a shell command string and returns its argv words.
//
// This is intentionally conservative: it only succeeds for a single simple
// command without redirections, pipelines, boolean operators, variable
// expansions, command substitutions, or other shell-specific constructs.
//
// This is useful for suggesting "exec form" JSON arrays for Dockerfile
// instructions like CMD/ENTRYPOINT when the shell form is trivially
// tokenizable.
func SplitSimpleCommand(cmd string, variant Variant) ([]string, bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil, false
	}

	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	file, err := parser.Parse(strings.NewReader(cmd), "")
	if err != nil {
		return nil, false
	}

	// Require exactly one statement (no pipelines/blocks/conditionals).
	if len(file.Stmts) != 1 {
		return nil, false
	}
	stmt := file.Stmts[0]
	if stmt == nil || stmt.Cmd == nil {
		return nil, false
	}
	if len(stmt.Redirs) > 0 || stmt.Background || stmt.Negated {
		return nil, false
	}

	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return nil, false
	}
	if len(call.Assigns) > 0 {
		return nil, false
	}
	if len(call.Args) == 0 {
		return nil, false
	}

	args := make([]string, 0, len(call.Args))
	for _, w := range call.Args {
		s, ok := simpleLiteralWord(w)
		if !ok {
			return nil, false
		}
		args = append(args, s)
	}

	return args, true
}

// simpleLiteralWord renders a word that is made entirely of literals and quotes,
// rejecting any shell expansions or other constructs. It also rejects unquoted
// glob metacharacters, since exec-form JSON will not perform glob expansion.
func simpleLiteralWord(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	if len(w.Parts) == 0 {
		return "", false
	}
	// Reject leading-tilde words (e.g. "~", "~/bin", "~user/bin") since shell form
	// would expand them but exec-form JSON won't.
	if first, ok := w.Parts[0].(*syntax.Lit); ok && strings.HasPrefix(first.Value, "~") {
		return "", false
	}

	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			// Reject unquoted glob metacharacters; shell form would expand them.
			if strings.ContainsAny(p.Value, "*?[]") {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				lit, ok := dp.(*syntax.Lit)
				if !ok {
					// Disallow $var, `cmd`, $(cmd), etc.
					return "", false
				}
				b.WriteString(lit.Value)
			}
		default:
			// Disallow any other constructs (params, globs, cmd subst, etc).
			return "", false
		}
	}

	return b.String(), true
}
