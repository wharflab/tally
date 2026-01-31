// Package shell provides shell script parsing utilities for Dockerfile linting.
package shell

import (
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// PackageManager identifies a system package manager.
type PackageManager string

const (
	PackageManagerApt     PackageManager = "apt"
	PackageManagerApk     PackageManager = "apk"
	PackageManagerYum     PackageManager = "yum"
	PackageManagerDnf     PackageManager = "dnf"
	PackageManagerZypper  PackageManager = "zypper"
	PackageManagerPacman  PackageManager = "pacman"
	PackageManagerEmerge  PackageManager = "emerge"
	PackageManagerUnknown PackageManager = ""
)

// PackageInstallInfo represents a detected package installation.
type PackageInstallInfo struct {
	Manager  PackageManager
	Packages []string
}

// packageManagerInfo describes how to parse a package manager command.
type packageManagerInfo struct {
	// commands that trigger install mode (e.g., "install" for apt-get install)
	installCommands []string
	// whether this manager uses subcommands (apt-get install vs apk add)
	hasSubcommand bool
	// for managers without subcommand, the base command itself (e.g., "emerge")
	directInstall bool
}

var packageManagers = map[string]struct {
	manager PackageManager
	info    packageManagerInfo
}{
	"apt-get": {PackageManagerApt, packageManagerInfo{
		installCommands: []string{"install"},
		hasSubcommand:   true,
	}},
	"apt": {PackageManagerApt, packageManagerInfo{
		installCommands: []string{"install"},
		hasSubcommand:   true,
	}},
	"apk": {PackageManagerApk, packageManagerInfo{
		installCommands: []string{"add"},
		hasSubcommand:   true,
	}},
	"yum": {PackageManagerYum, packageManagerInfo{
		installCommands: []string{"install"},
		hasSubcommand:   true,
	}},
	"dnf": {PackageManagerDnf, packageManagerInfo{
		installCommands: []string{"install"},
		hasSubcommand:   true,
	}},
	"zypper": {PackageManagerZypper, packageManagerInfo{
		installCommands: []string{"install", "in"},
		hasSubcommand:   true,
	}},
	"pacman": {PackageManagerPacman, packageManagerInfo{
		// pacman uses flags like -S, -Sy, -Syu for install
		installCommands: []string{"-S", "-Sy", "-Syu", "-Syyu"},
		hasSubcommand:   true,
	}},
	"emerge": {PackageManagerEmerge, packageManagerInfo{
		directInstall: true, // emerge packages directly
	}},
}

// ExtractPackageInstalls parses a shell script and extracts package installations.
func ExtractPackageInstalls(script string, variant Variant) []PackageInstallInfo {
	parser := syntax.NewParser(
		syntax.Variant(variant.toLangVariant()),
		syntax.KeepComments(false),
	)

	prog, err := parser.Parse(strings.NewReader(script), "")
	if err != nil {
		// Fall back to simple extraction on parse error
		return extractPackageInstallsSimple(script)
	}

	var installs []PackageInstallInfo

	syntax.Walk(prog, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}

		// Get the command name
		cmdName := call.Args[0].Lit()
		if cmdName == "" {
			return true
		}
		cmdName = path.Base(cmdName)

		// Check if it's a known package manager
		pmInfo, found := packageManagers[cmdName]
		if !found {
			return true
		}

		// Extract the arguments as strings
		args := make([]string, 0, len(call.Args)-1)
		for _, arg := range call.Args[1:] {
			if lit := arg.Lit(); lit != "" {
				args = append(args, lit)
			}
		}

		// Parse the package list based on manager type
		packages := extractPackagesFromArgs(args, pmInfo.info)
		if len(packages) > 0 {
			installs = append(installs, PackageInstallInfo{
				Manager:  pmInfo.manager,
				Packages: packages,
			})
		}

		return true
	})

	return installs
}

// extractPackagesFromArgs extracts package names from command arguments.
func extractPackagesFromArgs(args []string, info packageManagerInfo) []string {
	if len(args) == 0 {
		return nil
	}

	// For direct install managers (emerge), all non-flag args are packages
	if info.directInstall {
		return filterPackageArgs(args)
	}

	// For managers with subcommands, find the install command first
	installIdx := -1
	for i, arg := range args {
		if slices.Contains(info.installCommands, arg) {
			installIdx = i
			break
		}
	}

	if installIdx < 0 {
		return nil // Not an install command
	}

	// Everything after the install subcommand (excluding flags) is a package
	return filterPackageArgs(args[installIdx+1:])
}

// filterPackageArgs filters out flags and options from argument list.
func filterPackageArgs(args []string) []string {
	packages := make([]string, 0, len(args))
	skipNext := false

	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}

		// Skip flags
		if strings.HasPrefix(arg, "-") {
			// Some flags take a value (e.g., -o option=value, --option value)
			// Skip common flags that take a following argument
			if arg == "-o" || arg == "-t" || arg == "--option" ||
				arg == "--target-release" {
				skipNext = true
			}
			continue
		}

		// Skip common non-package arguments
		if arg == "&&" || arg == "||" || arg == ";" {
			break // End of this command
		}

		packages = append(packages, arg)
	}

	return packages
}

// extractPackageInstallsSimple is a fallback for when AST parsing fails.
func extractPackageInstallsSimple(script string) []PackageInstallInfo {
	var installs []PackageInstallInfo

	// Simple pattern matching for common package install commands
	patterns := []struct {
		prefix  string
		manager PackageManager
	}{
		{"apt-get install", PackageManagerApt},
		{"apt install", PackageManagerApt},
		{"apk add", PackageManagerApk},
		{"yum install", PackageManagerYum},
		{"dnf install", PackageManagerDnf},
	}

	for _, p := range patterns {
		if idx := strings.Index(script, p.prefix); idx >= 0 {
			// Found a pattern, try to extract packages
			rest := script[idx+len(p.prefix):]
			packages := extractSimplePackages(rest)
			if len(packages) > 0 {
				installs = append(installs, PackageInstallInfo{
					Manager:  p.manager,
					Packages: packages,
				})
			}
		}
	}

	return installs
}

// extractSimplePackages extracts package names from a simple string.
func extractSimplePackages(s string) []string {
	packages := make([]string, 0, 8)
	for field := range strings.FieldsSeq(s) {
		// Stop at shell operators
		if field == "&&" || field == "||" || field == ";" || field == "|" {
			break
		}
		// Skip flags
		if strings.HasPrefix(field, "-") {
			continue
		}
		// Skip obvious non-packages
		if strings.Contains(field, "=") || strings.Contains(field, "$") {
			continue
		}
		packages = append(packages, field)
	}
	return packages
}
