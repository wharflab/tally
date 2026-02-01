package shell

import (
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CommandOccurrence represents a command with its exact position in the script.
type CommandOccurrence struct {
	// Name is the command name (e.g., "apt", "sudo").
	Name string

	// Subcommand is the first argument if it looks like a subcommand (e.g., "install" in "apt install").
	// Empty if the first argument looks like a flag or there are no arguments.
	Subcommand string

	// StartCol is the 0-based column offset where the command starts.
	StartCol int

	// EndCol is the 0-based column offset where the command name ends (exclusive).
	EndCol int

	// Line is the 0-based line number within the script where the command appears.
	Line int
}

// FindCommandOccurrences extracts all command positions from a shell script.
// It returns occurrences with precise byte offsets for each command found.
func FindCommandOccurrences(script string, variant Variant) []CommandOccurrence {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		// If parsing fails, return empty - we can't get precise positions
		return nil
	}

	var occurrences []CommandOccurrence
	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		// Get the first word (command name)
		cmdWord := call.Args[0]
		name := cmdWord.Lit()
		if name == "" {
			return true
		}

		// Strip path prefix (e.g., /usr/bin/apt -> apt)
		baseName := path.Base(name)

		// Calculate position - syntax.Pos uses 1-based line/column
		pos := cmdWord.Pos()
		endPos := cmdWord.End()

		occ := CommandOccurrence{
			Name:     baseName,
			StartCol: int(pos.Col()) - 1,   //nolint:gosec // G115: shell scripts won't have int-overflowing positions
			EndCol:   int(endPos.Col()) - 1, //nolint:gosec // G115: shell scripts won't have int-overflowing positions
			Line:     int(pos.Line()) - 1,   //nolint:gosec // G115: shell scripts won't have int-overflowing positions
		}

		// Extract subcommand (first non-flag argument)
		if len(call.Args) > 1 {
			for _, arg := range call.Args[1:] {
				argLit := arg.Lit()
				if argLit == "" || strings.HasPrefix(argLit, "-") {
					continue
				}
				occ.Subcommand = argLit
				break
			}
		}

		occurrences = append(occurrences, occ)

		// Handle command wrappers - extract wrapped command positions
		if commandWrappers[baseName] {
			wrapped := extractWrappedOccurrences(call.Args[1:], variant, baseName)
			occurrences = append(occurrences, wrapped...)
		}

		// Handle shell wrappers (sh -c, bash -c)
		if shellWrappers[baseName] {
			nested := extractNestedShellOccurrences(call.Args[1:], variant, pos)
			occurrences = append(occurrences, nested...)
		}

		return true
	})

	return occurrences
}

// wrapperOptionsWithValues maps wrapper commands to their flags that consume the next argument.
// These flags take a value as a separate argument (not with =), so we need to skip that value.
var wrapperOptionsWithValues = map[string]map[string]bool{
	"sudo": {
		"-u": true, "--user": true,
		"-g": true, "--group": true,
		"-h": true, "--host": true,
		"-p": true, "--prompt": true,
		"-r": true, "--role": true,
		"-t": true, "--type": true,
		"-U": true, "--other-user": true,
		"-C": true, "--close-from": true,
		"-D": true, "--chdir": true,
		"-R": true, "--chroot": true,
		"-T": true, "--command-timeout": true,
	},
	"env": {
		"-u": true, "--unset": true,
		"-C": true, "--chdir": true,
		"-S": true, "--split-string": true,
	},
	"nice": {
		"-n": true, "--adjustment": true,
	},
	"ionice": {
		"-c": true, "--class": true,
		"-n": true, "--classdata": true,
		"-p": true, "--pid": true,
		"-P": true, "--pgid": true,
		"-u": true, "--uid": true,
	},
	"timeout": {
		"-k": true, "--kill-after": true,
		"-s": true, "--signal": true,
	},
}

// extractWrappedOccurrences extracts command positions from wrapper arguments.
// wrapperName is the name of the wrapper command (e.g., "sudo", "env") to handle
// wrapper-specific options that consume the next argument.
func extractWrappedOccurrences(args []*syntax.Word, variant Variant, wrapperName string) []CommandOccurrence {
	occurrences := make([]CommandOccurrence, 0, 2) // Most wrappers have 1-2 commands
	skipNext := false

	// Get the options that consume values for this wrapper
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
				// Short flags without known mapping - assume they might take a value
				// This is a heuristic: single-char flags like -n often take values
				skipNext = true
			}
			continue
		}
		if strings.Contains(lit, "=") || isNumeric(lit) {
			continue
		}

		// Found a command
		name := path.Base(lit)
		pos := arg.Pos()
		endPos := arg.End()

		occ := CommandOccurrence{
			Name:     name,
			StartCol: int(pos.Col()) - 1,   //nolint:gosec // G115: shell scripts won't have int-overflowing positions
			EndCol:   int(endPos.Col()) - 1, //nolint:gosec // G115: shell scripts won't have int-overflowing positions
			Line:     int(pos.Line()) - 1,   //nolint:gosec // G115: shell scripts won't have int-overflowing positions
		}

		// Get subcommand from remaining args
		remainingArgs := args[i+1:]
		for _, ra := range remainingArgs {
			raLit := ra.Lit()
			if raLit == "" || strings.HasPrefix(raLit, "-") {
				continue
			}
			occ.Subcommand = raLit
			break
		}

		occurrences = append(occurrences, occ)

		// Recurse for nested wrappers
		if commandWrappers[name] {
			occurrences = append(occurrences, extractWrappedOccurrences(remainingArgs, variant, name)...)
		}
		if shellWrappers[name] {
			occurrences = append(occurrences, extractNestedShellOccurrences(remainingArgs, variant, arg.Pos())...)
		}
		break
	}

	return occurrences
}

// extractNestedShellOccurrences extracts command positions from "sh -c 'code'" patterns.
// Note: positions within nested shell code are relative to the quoted string, not the original script.
// This is a limitation that would require additional offset tracking to resolve.
func extractNestedShellOccurrences(args []*syntax.Word, variant Variant, _ syntax.Pos) []CommandOccurrence {
	foundDashC := false
	for _, arg := range args {
		lit := arg.Lit()
		if lit == "-c" {
			foundDashC = true
			continue
		}
		if strings.HasPrefix(lit, "-") && strings.Contains(lit, "c") {
			foundDashC = true
			continue
		}
		if foundDashC {
			code := extractQuotedContent(arg)
			if code != "" {
				// Parse nested shell code
				// Note: positions are relative to the nested code, not adjusted for parent
				return FindCommandOccurrences(code, variant)
			}
			break
		}
	}
	return nil
}

// FindCommandOccurrence finds the first occurrence of a specific command.
// Returns nil if the command is not found.
func FindCommandOccurrence(script, command string, variant Variant) *CommandOccurrence {
	for _, occ := range FindCommandOccurrences(script, variant) {
		if occ.Name == command {
			return &occ
		}
	}
	return nil
}

// FindAllCommandOccurrences finds all occurrences of a specific command.
func FindAllCommandOccurrences(script, command string, variant Variant) []CommandOccurrence {
	var matches []CommandOccurrence
	for _, occ := range FindCommandOccurrences(script, variant) {
		if occ.Name == command {
			matches = append(matches, occ)
		}
	}
	return matches
}
