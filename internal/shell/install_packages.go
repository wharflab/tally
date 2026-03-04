package shell

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// PackageArg represents a single package argument with its position in the source text.
type PackageArg struct {
	Value    string // literal text (e.g., "curl", "flask==2.0", "${PKG}")
	Line     int    // 0-based line within the source text
	StartCol int    // 0-based start column (byte offset)
	EndCol   int    // 0-based end column (byte offset, exclusive)
	IsVar    bool   // true if the argument contains a variable reference ($)
}

// InstallCommand represents a detected package install command with per-argument positions.
type InstallCommand struct {
	Manager  string       // e.g., "apt-get", "npm", "pip"
	Packages []PackageArg // non-flag args after subcommand, with positions
}

// installManagerInfo describes how to parse a package manager command for sorting.
// This extends the set from packages.go with npm/pip/etc. language managers.
type installManagerInfo struct {
	installCommands []string
}

// installManagers maps command names to their install subcommands.
// Reuses the set from prefer-package-cache-mounts + packages.go.
var installManagers = map[string]installManagerInfo{
	"apt-get":  {installCommands: []string{"install"}},
	"apt":      {installCommands: []string{"install"}},
	"apk":      {installCommands: []string{"add"}},
	"dnf":      {installCommands: []string{"install"}},
	"yum":      {installCommands: []string{"install"}},
	"zypper":   {installCommands: []string{"install", "in"}},
	"npm":      {installCommands: []string{"install", "i", "add"}},
	"yarn":     {installCommands: []string{"add"}},
	"pnpm":     {installCommands: []string{"add", "install", "i"}},
	"pip":      {installCommands: []string{"install"}},
	"pip3":     {installCommands: []string{"install"}},
	"bun":      {installCommands: []string{"add", "install", "i"}},
	"composer": {installCommands: []string{"require"}},
}

// pipFileArgs are pip arguments that indicate file-based install (skip sorting).
var pipFileArgs = map[string]bool{
	"-r":                true,
	"--requirement":     true,
	"-e":                true,
	"--editable":        true,
	"-c":                true,
	"--constraint":      true,
	"--no-deps":         false, // not a file arg, just listed for clarity
	".":                 true,
	"./":                true,
	"--find-links":      true,
	"--index-url":       true,
	"--extra-index-url": true,
}

// FindInstallPackages parses a shell script and extracts install commands with
// per-argument position information. Positions are 0-based line and column offsets
// within the source text.
func FindInstallPackages(script string, variant Variant) []InstallCommand {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		return nil
	}

	var commands []InstallCommand

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		cmdWord := call.Args[0]
		cmdName := cmdWord.Lit()
		if cmdName == "" {
			return true
		}
		cmdName = path.Base(cmdName)

		mgr, found := installManagers[cmdName]
		if !found {
			// Check wrapped commands (env, nice, etc.)
			if commandWrappers[cmdName] {
				commands = append(commands, findWrappedInstallPackages(call.Args[1:], variant, cmdName)...)
			}
			return true
		}

		cmd := extractInstallCommand(cmdName, mgr, call.Args[1:])
		if cmd != nil {
			commands = append(commands, *cmd)
		}

		return true
	})

	return commands
}

// extractInstallCommand extracts package arguments with positions from a call expression.
func extractInstallCommand(cmdName string, mgr installManagerInfo, args []*syntax.Word) *InstallCommand {
	// Find the install subcommand
	installIdx := -1
	for i, arg := range args {
		lit := arg.Lit()
		if lit == "" {
			continue
		}
		if slices.Contains(mgr.installCommands, lit) {
			installIdx = i
			break
		}
	}
	if installIdx < 0 {
		return nil
	}

	// Check for pip file-based installs
	isPip := cmdName == "pip" || cmdName == "pip3"
	if isPip {
		for _, arg := range args[installIdx+1:] {
			lit := arg.Lit()
			if lit == "" {
				continue
			}
			if pipFileArgs[lit] {
				return nil // file-based install, skip
			}
			// Check for paths like ".", "./foo", "/path/to/pkg"
			if !strings.HasPrefix(lit, "-") && (strings.HasPrefix(lit, ".") || strings.HasPrefix(lit, "/")) {
				return nil
			}
		}
	}

	// Extract package arguments (non-flags) with positions
	packages := make([]PackageArg, 0, len(args)-installIdx)
	skipNext := false
	for _, arg := range args[installIdx+1:] {
		if skipNext {
			skipNext = false
			continue
		}

		// Get the text representation of the argument
		argText := wordText(arg)
		if argText == "" {
			continue
		}

		// Skip flags
		if strings.HasPrefix(argText, "-") {
			// Some flags take a following argument
			if isFlagWithValue(argText) {
				skipNext = true
			}
			continue
		}

		pos := arg.Pos()
		endPos := arg.End()

		packages = append(packages, PackageArg{
			Value:    argText,
			Line:     int(pos.Line()) - 1,   //nolint:gosec // shell positions won't overflow
			StartCol: int(pos.Col()) - 1,    //nolint:gosec
			EndCol:   int(endPos.Col()) - 1, //nolint:gosec
			IsVar:    strings.Contains(argText, "$"),
		})
	}

	if len(packages) == 0 {
		return nil
	}

	return &InstallCommand{
		Manager:  cmdName,
		Packages: packages,
	}
}

// findWrappedInstallPackages finds install commands within wrapper arguments.
func findWrappedInstallPackages(args []*syntax.Word, variant Variant, wrapperName string) []InstallCommand {
	var commands []InstallCommand

	IterateWrapperArgs(args, wrapperName, func(wa WrapperArg) bool {
		mgr, found := installManagers[wa.Name]
		if found {
			allArgs := append([]*syntax.Word{wa.Arg}, wa.RemainingArgs...)
			// The first element is the command name itself, skip it
			cmd := extractInstallCommand(wa.Name, mgr, wa.RemainingArgs)
			_ = allArgs // suppress unused
			if cmd != nil {
				commands = append(commands, *cmd)
			}
		}

		// Recurse for nested wrappers
		if commandWrappers[wa.Name] {
			commands = append(commands, findWrappedInstallPackages(wa.RemainingArgs, variant, wa.Name)...)
		}
		return true
	})

	return commands
}

// wordText extracts the full text representation of a shell word,
// including variable references like ${PKG}.
func wordText(w *syntax.Word) string {
	// For simple literals, use Lit()
	if lit := w.Lit(); lit != "" {
		return lit
	}

	// Build text from parts, including variable expansions
	var sb strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			sb.WriteString(p.Value)
		case *syntax.SglQuoted:
			sb.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dpart := range p.Parts {
				switch dp := dpart.(type) {
				case *syntax.Lit:
					sb.WriteString(dp.Value)
				case *syntax.ParamExp:
					sb.WriteByte('$')
					if dp.Short {
						sb.WriteString(dp.Param.Value)
					} else {
						sb.WriteByte('{')
						sb.WriteString(dp.Param.Value)
						sb.WriteByte('}')
					}
				}
			}
		case *syntax.ParamExp:
			sb.WriteByte('$')
			if p.Short {
				sb.WriteString(p.Param.Value)
			} else {
				sb.WriteByte('{')
				sb.WriteString(p.Param.Value)
				sb.WriteByte('}')
			}
		}
	}
	return sb.String()
}

// isFlagWithValue returns true for flags that consume the next argument as a value.
func isFlagWithValue(flag string) bool {
	// Long flags with = are self-contained and don't consume the next argument.
	if strings.Contains(flag, "=") {
		return false
	}

	// Common flags that take a value from the next argument.
	switch flag {
	case "-o", "--option",
		"-t", "--target-release",
		"--root", "--installroot",
		"--prefix", "--target":
		return true
	}

	// By default, assume other flags are standalone (e.g., -y, --no-cache, --save-dev).
	return false
}
