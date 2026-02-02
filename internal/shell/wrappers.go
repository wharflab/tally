package shell

import (
	"path"
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
			// Only skip the next argument when the flag is known to consume a value.
			// Unknown short flags (like -k, -i) are assumed to be boolean.
			if optionsWithValues != nil && optionsWithValues[lit] {
				skipNext = true
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
			Name:          path.Base(lit),
			RemainingArgs: args[i+1:],
		}

		if callback(wa) {
			break
		}
	}
}
