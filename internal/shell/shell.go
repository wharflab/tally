// Package shell provides shell script parsing utilities for Dockerfile linting.
// It wraps mvdan.cc/sh/v3/syntax to provide a simple API for extracting
// command names from shell scripts, similar to how hadolint uses ShellCheck.
package shell

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/fileutil"
	"mvdan.cc/sh/v3/syntax"
)

// Variant represents a shell variant for parsing and rule gating.
// Each variant expresses what the shell can do, enabling rules to query
// capabilities via intent-based methods rather than negation tests.
type Variant int

const (
	// VariantBash is the GNU Bash shell (default for Docker Linux containers).
	VariantBash Variant = iota
	// VariantPOSIX is the POSIX-compliant shell (sh, dash, ash).
	VariantPOSIX
	// VariantMksh is the MirBSD Korn Shell.
	VariantMksh
	// VariantZsh is the Z shell.
	VariantZsh
	// VariantPowerShell is PowerShell (cross-platform: powershell on Windows, pwsh on Linux/macOS).
	VariantPowerShell
	// VariantCmd is the Windows cmd.exe command interpreter.
	VariantCmd
	// VariantUnknown is an unrecognized shell. Treated conservatively: not parseable,
	// not ShellCheck-compatible, not PowerShell.
	VariantUnknown
)

// IsParseable returns true for shells whose scripts can be parsed by mvdan.cc/sh.
// Use this to guard shell AST analysis, command extraction, and chained-command counting.
func (v Variant) IsParseable() bool {
	return v == VariantBash || v == VariantPOSIX || v == VariantMksh || v == VariantZsh
}

// IsShellCheckCompatible returns true for shells that ShellCheck can analyze.
// Use this to guard ShellCheck WASM invocation.
func (v Variant) IsShellCheckCompatible() bool {
	return v == VariantBash || v == VariantPOSIX || v == VariantMksh || v == VariantZsh
}

// SupportsHeredoc returns true for shells compatible with BuildKit heredoc syntax (RUN <<EOF).
// Use this to guard heredoc suggestions and fixes.
func (v Variant) SupportsHeredoc() bool {
	return v == VariantBash || v == VariantPOSIX || v == VariantMksh || v == VariantZsh
}

// IsPowerShell returns true for PowerShell variants (powershell, pwsh).
// Use this to gate PowerShell-specific lint rules (tally/powershell/*).
func (v Variant) IsPowerShell() bool {
	return v == VariantPowerShell
}

// VariantFromShell returns the appropriate Variant for a shell name.
// Common shell mappings:
//   - bash -> VariantBash
//   - sh, dash, ash -> VariantPOSIX
//   - mksh, ksh -> VariantMksh
//   - zsh -> VariantZsh
//   - powershell, pwsh -> VariantPowerShell
//   - cmd -> VariantCmd
//   - unknown -> VariantUnknown
func VariantFromShell(shell string) Variant {
	// Normalize: extract basename, lowercase, strip .exe suffix (for Windows shells).
	// Replace backslashes before path.Base so Windows paths like
	// "C:\\Windows\\System32\\...\\powershell.exe" are handled correctly
	// (path.Base only recognises forward slashes).
	shell = strings.ToLower(path.Base(strings.ReplaceAll(shell, `\`, "/")))
	shell = strings.TrimSuffix(shell, ".exe")

	switch shell {
	case "bash":
		return VariantBash
	case "sh", "dash", "ash":
		return VariantPOSIX
	case "mksh", "ksh":
		return VariantMksh
	case "zsh":
		return VariantZsh
	case "powershell", "pwsh":
		return VariantPowerShell
	case "cmd":
		return VariantCmd
	default:
		return VariantUnknown
	}
}

// ShellFromShebang extracts the shell name from a shebang line.
// It delegates to [fileutil.Shebang] for the common cases (sh, bash, mksh,
// bats, zsh) and adds ksh support for Dockerfile compatibility.
//
// Returns the normalized shell name (e.g., "bash", "sh", "ksh") and true
// if a known shell shebang was found. The returned name can be passed
// directly to [VariantFromShell].
func ShellFromShebang(line string) (string, bool) {
	if s := fileutil.Shebang([]byte(line)); s != "" {
		return s, true
	}
	// fileutil.Shebang covers sh/bash/mksh/bats/zsh but not plain ksh.
	// Handle #!/bin/ksh and #!/usr/bin/env ksh for Dockerfile heredoc support.
	if !strings.HasPrefix(line, "#!") {
		return "", false
	}
	rest := strings.TrimSpace(line[2:])
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return "", false
	}
	name := parts[0]
	if len(parts) >= 2 && path.Base(parts[0]) == "env" {
		name = parts[1]
	}
	const kshName = "ksh"
	if path.Base(name) == kshName {
		return kshName, true
	}
	return "", false
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
// Only meaningful for parseable variants; callers should check IsParseable() first.
func (v Variant) toLangVariant() syntax.LangVariant {
	switch v {
	case VariantBash:
		return syntax.LangBash
	case VariantPOSIX:
		return syntax.LangPOSIX
	case VariantMksh:
		return syntax.LangMirBSDKorn
	case VariantZsh:
		return syntax.LangZsh
	case VariantPowerShell, VariantCmd, VariantUnknown:
		// Can't be parsed by mvdan.cc/sh. Fallback to Bash —
		// callers should have checked IsParseable() first.
		return syntax.LangBash
	}
	return syntax.LangBash
}

// CommandNames extracts all command names from a shell script.
// Uses VariantBash by default. Use CommandNamesWithVariant for other shells.
func CommandNames(script string) []string {
	return CommandNamesWithVariant(script, VariantBash)
}

// commandWrappers are commands that execute another command passed as arguments.
// When we see these commands, we look at their arguments to find the wrapped command.
var commandWrappers = map[string]bool{
	"env":     true, // env [-i] [NAME=VALUE]... COMMAND [ARG]...
	"nice":    true, // nice [-n ADJUSTMENT] COMMAND [ARG]...
	"nohup":   true, // nohup COMMAND [ARG]...
	"ionice":  true, // ionice [-c CLASS] [-n LEVEL] COMMAND [ARG]...
	"strace":  true, // strace [OPTIONS] COMMAND [ARG]...
	"timeout": true, // timeout DURATION COMMAND [ARG]...
	"watch":   true, // watch [-n INTERVAL] COMMAND [ARG]...
	"xargs":   true, // xargs [OPTIONS] COMMAND [ARG]...
	"exec":    true, // exec COMMAND [ARG]...
	"builtin": true, // builtin COMMAND [ARG]...
	"command": true, // command COMMAND [ARG]...
}

// shellWrappers are commands that execute shell code passed as a string argument.
// When we see "bash -c 'code'" or "sh -c 'code'", we parse the string as shell code.
var shellWrappers = map[string]bool{
	"sh":   true,
	"bash": true,
	"dash": true,
	"ash":  true,
	"zsh":  true,
	"ksh":  true,
}

// CommandNamesWithVariant extracts all command names from a shell script
// using the specified shell variant for parsing.
//
// It parses the script and walks the AST to find all CallExpr nodes,
// returning the first word of each (the command name). It also handles
// command wrappers (env, nice, xargs, etc.) and shell wrappers (sh -c, bash -c).
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

				// Handle command wrappers (env, nice, xargs, etc.)
				if commandWrappers[name] {
					wrappedNames := extractWrappedCommands(call.Args[1:], variant)
					names = append(names, wrappedNames...)
				}

				// Handle shell wrappers (sh -c, bash -c, etc.)
				if shellWrappers[name] {
					nestedNames := extractShellWrapperCommands(call.Args[1:], variant)
					names = append(names, nestedNames...)
				}
			}
		}
		return true
	})

	return names
}

// extractWrappedCommands extracts command names from wrapper arguments.
// It skips flags and environment assignments to find the actual wrapped command.
func extractWrappedCommands(args []*syntax.Word, variant Variant) []string {
	names := make([]string, 0, 2) // typically 0-2 wrapped commands
	skipNext := false
	for i, arg := range args {
		lit := arg.Lit()
		if lit == "" {
			continue
		}
		// Skip arguments to previous flag (e.g., 10 in "-n 10")
		if skipNext {
			skipNext = false
			continue
		}
		// Skip flags, and mark that the next arg might be a flag value
		if strings.HasPrefix(lit, "-") {
			// Flags like -n, -c, -p often take a value argument
			skipNext = len(lit) == 2 && lit != "--"
			continue
		}
		// Skip environment assignments (FOO=bar)
		if strings.Contains(lit, "=") {
			continue
		}
		// Skip pure numeric arguments (likely a value for a flag like timeout)
		if isNumeric(lit) {
			continue
		}
		// Found a command name
		name := path.Base(lit)
		names = append(names, name)

		// If this wrapped command is also a wrapper, recurse
		remainingArgs := args[i+1:]
		if commandWrappers[name] {
			names = append(names, extractWrappedCommands(remainingArgs, variant)...)
		}
		if shellWrappers[name] {
			names = append(names, extractShellWrapperCommands(remainingArgs, variant)...)
		}
		break // Only look for first command
	}
	return names
}

// isNumeric returns true if the string contains only digits (and optional leading sign).
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	start := 0
	if s[0] == '-' || s[0] == '+' {
		start = 1
	}
	for i := start; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return start < len(s)
}

// extractQuotedContent extracts the string content from a shell word,
// handling single quotes, double quotes, and unquoted literals.
func extractQuotedContent(word *syntax.Word) string {
	// First try the simple literal case
	if lit := word.Lit(); lit != "" {
		return lit
	}

	// Otherwise, extract from quoted parts
	var sb strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			// For double quotes, extract literal parts
			for _, dpart := range p.Parts {
				if lit, ok := dpart.(*syntax.Lit); ok {
					sb.WriteString(lit.Value)
				}
			}
		case *syntax.Lit:
			sb.WriteString(p.Value)
		}
	}
	return sb.String()
}

// extractShellWrapperCommands extracts commands from "sh -c 'code'" patterns.
// It looks for -c flag followed by a string argument containing shell code.
func extractShellWrapperCommands(args []*syntax.Word, variant Variant) []string {
	var names []string
	foundDashC := false
	for _, arg := range args {
		lit := arg.Lit()
		if lit == "-c" {
			foundDashC = true
			continue
		}
		// Also handle -c combined with other flags (e.g., -ec, -xc)
		if strings.HasPrefix(lit, "-") && strings.Contains(lit, "c") {
			foundDashC = true
			continue
		}
		if foundDashC {
			// Try to get shell code from the argument
			code := extractQuotedContent(arg)
			if code != "" {
				names = append(names, CommandNamesWithVariant(code, variant)...)
			}
			break
		}
	}
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

// SetsErrorFlag checks if a command is a "set" builtin that enables the -e flag.
// Uses shell AST to properly detect any flag combination containing 'e'
// (e.g., "set -e", "set -ex", "set -euo pipefail").
func SetsErrorFlag(cmd string, variant Variant) bool {
	setCmds := FindCommands(cmd, variant, "set")
	for _, setCmd := range setCmds {
		if setCmd.HasFlag("e") {
			return true
		}
	}
	return false
}
