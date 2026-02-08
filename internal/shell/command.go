// Package shell provides shell script parsing utilities for Dockerfile linting.
package shell

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CommandInfo represents a parsed command with its arguments and flags.
type CommandInfo struct {
	// Name is the base command name (e.g., "apt-get", "yum").
	Name string

	// Subcommand is the first non-flag argument (e.g., "install" in "apt-get install").
	Subcommand string

	// Args contains all arguments including flags.
	Args []string

	// Position information for the command name.
	Line     int // 0-based line within the script
	StartCol int // 0-based column where command starts
	EndCol   int // 0-based column where command name ends

	// Position information for the subcommand (if present).
	// These are only set when Subcommand is non-empty.
	SubcommandLine     int // 0-based line within the script
	SubcommandStartCol int // 0-based column where subcommand starts
	SubcommandEndCol   int // 0-based column where subcommand ends
}

// HasFlag checks if the command has a specific flag.
// Handles both short flags (-y) and long flags (--yes).
// For short flags, also checks combined flags (e.g., -yq contains -y).
func (c *CommandInfo) HasFlag(flag string) bool {
	// Normalize flag - remove leading dashes for comparison
	normalizedFlag := strings.TrimLeft(flag, "-")
	isLong := strings.HasPrefix(flag, "--") || len(normalizedFlag) > 1

	for _, arg := range c.Args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}

		if isLong {
			// Long flag: exact match with --
			if arg == "--"+normalizedFlag {
				return true
			}
			// Handle --flag=value form
			if strings.HasPrefix(arg, "--"+normalizedFlag+"=") {
				return true
			}
		} else {
			// Short flag: check for exact -x or combined -xyz
			argWithoutDash := strings.TrimPrefix(arg, "-")
			// Skip if it's a long flag
			if strings.HasPrefix(arg, "--") {
				continue
			}
			// Check if the single-char flag is in the argument
			if strings.Contains(argWithoutDash, normalizedFlag) {
				return true
			}
		}
	}
	return false
}

// HasAnyFlag checks if the command has any of the specified flags.
func (c *CommandInfo) HasAnyFlag(flags ...string) bool {
	return slices.ContainsFunc(flags, c.HasFlag)
}

// CountFlag counts how many times a flag appears in the command.
// Useful for checking flags like -q -q (equivalent to -qq).
func (c *CommandInfo) CountFlag(flag string) int {
	normalizedFlag := strings.TrimLeft(flag, "-")
	isLong := strings.HasPrefix(flag, "--")
	count := 0

	for _, arg := range c.Args {
		if !strings.HasPrefix(arg, "-") {
			continue
		}

		if isLong {
			// Long flag: exact match
			if arg == "--"+normalizedFlag {
				count++
			}
		} else if len(normalizedFlag) == 1 {
			// Short flag: only count single-char flags in combined args
			if strings.HasPrefix(arg, "--") {
				continue
			}
			argWithoutDash := strings.TrimPrefix(arg, "-")
			// Count occurrences of the single char in combined flags
			count += strings.Count(argWithoutDash, normalizedFlag)
		}
	}
	return count
}

// HasAnyArg checks if any of the specified arguments are present as the subcommand.
func (c *CommandInfo) HasAnyArg(args ...string) bool {
	return slices.Contains(args, c.Subcommand)
}

// GetArgValue returns the value following a flag (e.g., "-q=2" returns "2").
// Returns empty string if not found or no value.
func (c *CommandInfo) GetArgValue(flag string) string {
	normalizedFlag := strings.TrimLeft(flag, "-")
	isLong := strings.HasPrefix(flag, "--")
	prefix := "-"
	if isLong {
		prefix = "--"
	}

	for i, arg := range c.Args {
		// Check for --flag=value or -f=value form
		if value, found := strings.CutPrefix(arg, prefix+normalizedFlag+"="); found {
			return value
		}
		// Check for --flag value or -f value form (next argument)
		if arg == prefix+normalizedFlag && i+1 < len(c.Args) {
			next := c.Args[i+1]
			if !strings.HasPrefix(next, "-") {
				return next
			}
		}
	}
	return ""
}

// FindCommands extracts all commands matching the given name(s) from a shell script.
// It returns detailed CommandInfo for each matching command.
func FindCommands(script string, variant Variant, names ...string) []CommandInfo {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil
	}

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	var commands []CommandInfo
	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		cmdWord := call.Args[0]
		name := cmdWord.Lit()
		if name == "" {
			return true
		}
		baseName := path.Base(name)

		if !nameSet[baseName] {
			// Check wrapped commands
			if commandWrappers[baseName] {
				wrapped := findWrappedCommands(call.Args[1:], variant, baseName, nameSet)
				commands = append(commands, wrapped...)
			}
			if shellWrappers[baseName] {
				nested := findNestedShellCommands(call.Args[1:], variant, nameSet)
				commands = append(commands, nested...)
			}
			return true
		}

		pos := cmdWord.Pos()
		endPos := cmdWord.End()

		info := CommandInfo{
			Name:     baseName,
			Line:     int(pos.Line()) - 1,   //nolint:gosec // shell positions won't overflow
			StartCol: int(pos.Col()) - 1,    //nolint:gosec
			EndCol:   int(endPos.Col()) - 1, //nolint:gosec
		}

		// Extract all arguments and find subcommand with position
		for _, arg := range call.Args[1:] {
			lit := extractQuotedContent(arg)
			if lit == "" {
				continue
			}
			info.Args = append(info.Args, lit)

			// Set subcommand (first non-flag argument) with position
			if info.Subcommand == "" && !strings.HasPrefix(lit, "-") {
				info.Subcommand = lit
				argPos := arg.Pos()
				argEndPos := arg.End()
				info.SubcommandLine = int(argPos.Line()) - 1     //nolint:gosec
				info.SubcommandStartCol = int(argPos.Col()) - 1  //nolint:gosec
				info.SubcommandEndCol = int(argEndPos.Col()) - 1 //nolint:gosec
			}
		}

		commands = append(commands, info)
		return true
	})

	return commands
}

// findWrappedCommands finds commands within wrapper arguments.
func findWrappedCommands(args []*syntax.Word, variant Variant, wrapperName string, nameSet map[string]bool) []CommandInfo {
	var commands []CommandInfo

	IterateWrapperArgs(args, wrapperName, func(wa WrapperArg) bool {
		if nameSet[wa.Name] {
			pos := wa.Arg.Pos()
			endPos := wa.Arg.End()

			info := CommandInfo{
				Name:     wa.Name,
				Line:     int(pos.Line()) - 1,   //nolint:gosec
				StartCol: int(pos.Col()) - 1,    //nolint:gosec
				EndCol:   int(endPos.Col()) - 1, //nolint:gosec
			}

			// Extract remaining args and find subcommand with position
			for _, ra := range wa.RemainingArgs {
				raLit := extractQuotedContent(ra)
				if raLit == "" {
					continue
				}
				info.Args = append(info.Args, raLit)

				// Set subcommand (first non-flag argument) with position
				if info.Subcommand == "" && !strings.HasPrefix(raLit, "-") {
					info.Subcommand = raLit
					raPos := ra.Pos()
					raEndPos := ra.End()
					info.SubcommandLine = int(raPos.Line()) - 1     //nolint:gosec
					info.SubcommandStartCol = int(raPos.Col()) - 1  //nolint:gosec
					info.SubcommandEndCol = int(raEndPos.Col()) - 1 //nolint:gosec
				}
			}

			commands = append(commands, info)
		}

		// Recurse for nested wrappers
		if commandWrappers[wa.Name] {
			commands = append(commands, findWrappedCommands(wa.RemainingArgs, variant, wa.Name, nameSet)...)
		}
		if shellWrappers[wa.Name] {
			commands = append(commands, findNestedShellCommands(wa.RemainingArgs, variant, nameSet)...)
		}
		return true // Break after first command found
	})

	return commands
}

// findNestedShellCommands finds commands within "sh -c 'code'" patterns.
func findNestedShellCommands(args []*syntax.Word, variant Variant, nameSet map[string]bool) []CommandInfo {
	foundDashC := false
	for _, arg := range args {
		lit := arg.Lit()
		if lit == "-c" {
			foundDashC = true
			continue
		}
		if strings.HasPrefix(lit, "-") && !strings.HasPrefix(lit, "--") && strings.ContainsRune(lit[1:], 'c') {
			foundDashC = true
			continue
		}
		if foundDashC {
			code := extractQuotedContent(arg)
			if code != "" {
				// Convert nameSet back to slice for recursive call
				names := make([]string, 0, len(nameSet))
				for n := range nameSet {
					names = append(names, n)
				}
				return FindCommands(code, variant, names...)
			}
			break
		}
	}
	return nil
}
