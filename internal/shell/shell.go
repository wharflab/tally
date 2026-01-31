// Package shell provides shell script parsing utilities for Dockerfile linting.
// It wraps mvdan.cc/sh/v3/syntax to provide a simple API for extracting
// command names from shell scripts, similar to how hadolint uses ShellCheck.
package shell

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Variant represents a shell variant for parsing.
type Variant int

const (
	// VariantBash is the GNU Bash shell (default for Docker).
	VariantBash Variant = iota
	// VariantPOSIX is the POSIX-compliant shell (sh, dash, ash).
	VariantPOSIX
	// VariantMksh is the MirBSD Korn Shell.
	VariantMksh
)

// VariantFromShell returns the appropriate Variant for a shell name.
// Common shell mappings:
//   - bash -> VariantBash
//   - sh, dash, ash -> VariantPOSIX
//   - mksh, ksh -> VariantMksh
//   - zsh -> VariantBash (closest approximation)
//   - unknown -> VariantBash (safe default)
func VariantFromShell(shell string) Variant {
	// Normalize: extract basename and lowercase
	shell = strings.ToLower(path.Base(shell))

	switch shell {
	case "bash":
		return VariantBash
	case "sh", "dash", "ash":
		return VariantPOSIX
	case "mksh", "ksh":
		return VariantMksh
	case "zsh":
		// zsh is mostly bash-compatible for our purposes
		return VariantBash
	default:
		// Default to bash for unknown shells
		return VariantBash
	}
}

// VariantFromShellCmd returns the appropriate Variant from a SHELL command array.
// The first element is typically the shell path (e.g., ["/bin/bash", "-c"]).
func VariantFromShellCmd(shellCmd []string) Variant {
	if len(shellCmd) == 0 {
		return VariantBash
	}
	return VariantFromShell(shellCmd[0])
}

// toLangVariant converts our Variant to mvdan.cc/sh's LangVariant.
func (v Variant) toLangVariant() syntax.LangVariant {
	switch v {
	case VariantBash:
		return syntax.LangBash
	case VariantPOSIX:
		return syntax.LangPOSIX
	case VariantMksh:
		return syntax.LangMirBSDKorn
	}
	return syntax.LangBash
}

// CommandNames extracts all command names from a shell script.
// Uses VariantBash by default. Use CommandNamesWithVariant for other shells.
func CommandNames(script string) []string {
	return CommandNamesWithVariant(script, VariantBash)
}

// CommandNamesWithVariant extracts all command names from a shell script
// using the specified shell variant for parsing.
//
// It parses the script and walks the AST to find all CallExpr nodes,
// returning the first word of each (the command name).
//
// This matches hadolint's behavior using ShellCheck.findCommandNames.
func CommandNamesWithVariant(script string, variant Variant) []string {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		// If parsing fails, fall back to simple word splitting
		return simpleCommandNames(script)
	}

	var names []string
	syntax.Walk(prog, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok && len(call.Args) > 0 {
			// Get the first word (command name)
			if name := call.Args[0].Lit(); name != "" {
				// Strip path prefix (e.g., /usr/bin/wget -> wget)
				name = path.Base(name)
				names = append(names, name)
			}
		}
		return true
	})

	return names
}

// ContainsCommand checks if a shell script contains a specific command.
// Uses VariantBash by default.
func ContainsCommand(script, command string) bool {
	return slices.Contains(CommandNames(script), command)
}

// ContainsCommandWithVariant checks if a shell script contains a specific command
// using the specified shell variant for parsing.
func ContainsCommandWithVariant(script, command string, variant Variant) bool {
	return slices.Contains(CommandNamesWithVariant(script, variant), command)
}

// simpleCommandNames is a fallback when parsing fails.
// It does basic word splitting to find potential command names.
func simpleCommandNames(script string) []string {
	var names []string

	// Replace shell operators with a marker to split on
	const marker = "\x00"
	for _, sep := range []string{"&&", "||", ";", "|", "`", "$("} {
		script = strings.ReplaceAll(script, sep, marker)
	}
	script = strings.ReplaceAll(script, "(", marker)
	script = strings.ReplaceAll(script, ")", " ")
	script = strings.ReplaceAll(script, "\\\n", " ")
	script = strings.ReplaceAll(script, "\n", marker)

	// Split by the marker to get individual command sequences
	for seq := range strings.SplitSeq(script, marker) {
		seq = strings.TrimSpace(seq)
		if seq == "" {
			continue
		}

		// Get the first non-assignment, non-flag token as the command
		for part := range strings.FieldsSeq(seq) {
			// Skip environment variable assignments (FOO=bar)
			if strings.Contains(part, "=") && !strings.HasPrefix(part, "-") {
				continue
			}
			// Skip flags
			if strings.HasPrefix(part, "-") {
				continue
			}
			// Strip path prefix
			names = append(names, path.Base(part))
			break
		}
	}

	return names
}
