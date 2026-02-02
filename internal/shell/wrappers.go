package shell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// WrapperArg represents a potential command argument found within wrapper arguments.
type WrapperArg struct {
	// Arg is the syntax.Word representing this argument
	Arg *syntax.Word
	// Index is the position in the args slice
	Index int
	// Name is the base name of the command (path.Base applied)
	Name string
	// RemainingArgs are the args after this command
	RemainingArgs []*syntax.Word
}

// IterateWrapperArgs iterates through wrapper command arguments, skipping flags and
// their values, and calls the callback for each potential command argument found.
// This handles the common pattern of finding commands within sudo, env, etc.
//
// The callback should return true to break iteration, false to continue looking
// for nested wrappers.
func IterateWrapperArgs(args []*syntax.Word, wrapperName string, callback func(WrapperArg) bool) {
	skipNext := false
	optionsWithValues := wrapperOptionsWithValues[wrapperName]

	for i, arg := range args {
		lit := arg.Lit()
		if lit == "" {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(lit, "-") {
			// Check if this flag consumes the next argument
			if optionsWithValues != nil && optionsWithValues[lit] {
				skipNext = true
			} else if len(lit) == 2 && lit != "--" {
				// Only skip if the next token doesn't look like a command/wrapper.
				// This avoids incorrectly skipping commands after boolean flags like -i or -k.
				if i+1 < len(args) {
					nextLit := args[i+1].Lit()
					nextName := pathBase(nextLit)
					if nextLit != "" && !commandWrappers[nextName] && !shellWrappers[nextName] {
						skipNext = true
					}
				}
			}
			continue
		}
		if strings.Contains(lit, "=") || isNumeric(lit) {
			continue
		}

		// Found a potential command
		wa := WrapperArg{
			Arg:           arg,
			Index:         i,
			Name:          pathBase(lit),
			RemainingArgs: args[i+1:],
		}

		if callback(wa) {
			break
		}
	}
}

// pathBase returns the base name from a path, handling empty strings.
// Equivalent to path.Base but avoids import for simple case.
func pathBase(p string) string {
	if p == "" {
		return ""
	}
	// Find last slash
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
