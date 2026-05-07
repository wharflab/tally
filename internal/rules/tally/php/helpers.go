package php

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/runcheck"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/stagename"
)

const (
	subcommandInstall = "install"

	cmdDockerPHPExtInstall = "docker-php-ext-install"
	cmdDockerPHPExtEnable  = "docker-php-ext-enable"
	cmdPecl                = "pecl"
)

// phpExtensionCommandNames are the PHP-specific extension commands that
// can install or enable a PHP extension. General OS package-manager
// installs are handled separately via facts.RunFacts.InstallCommands.
var phpExtensionCommandNames = []string{
	cmdDockerPHPExtInstall,
	cmdDockerPHPExtEnable,
	cmdPecl,
}

// stageLooksLikeDev is a thin wrapper around stagename.LooksLikeDev so the PHP
// rules continue to read naturally without leaking the import path. The
// classification itself lives in internal/stagename so other rule packages
// (e.g. tally/ruby) can share it.
func stageLooksLikeDev(name string) bool {
	return stagename.LooksLikeDev(name)
}

func composerNoDevEnabled(env facts.EnvFacts) bool {
	return composerNoDevEnabledValues(env.Values)
}

func composerNoDevEnabledValues(values map[string]string) bool {
	if len(values) == 0 {
		return false
	}
	return composerTruthy(values["COMPOSER_NO_DEV"])
}

func composerTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func composerInstallHasNoDev(cmd shell.CommandInfo) bool {
	return cmd.Subcommand == subcommandInstall && cmd.HasFlag("--no-dev")
}

func findComposerCommands(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	sm *sourcemap.SourceMap,
	subcommands ...string,
) ([]shell.CommandInfo, int) {
	cmds, runStartLine := runcheck.FindCommands(run, shellVariant, sm, "composer")
	if len(subcommands) == 0 {
		return cmds, runStartLine
	}

	filtered := make([]shell.CommandInfo, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd.HasAnyArg(subcommands...) {
			filtered = append(filtered, cmd)
		}
	}
	return filtered, runStartLine
}

func phpCommandLocation(
	file string,
	run *instructions.RunCommand,
	cmd shell.CommandInfo,
	runStartLine int,
) rules.Location {
	if runStartLine == 0 {
		return rules.NewLocationFromRanges(file, run.Location())
	}
	cmdLine := runStartLine + cmd.Line
	return rules.NewRangeLocation(file, cmdLine, cmd.StartCol, cmdLine, cmd.EndCol)
}

// findXdebugCommands returns RUN commands that install or enable Xdebug,
// covering both PHP-specific extension commands (docker-php-ext-install,
// docker-php-ext-enable, pecl install) and OS package-manager installs
// (apt/apt-get/apk/dnf/microdnf/yum/zypper) whose package list contains
// an Xdebug-bearing package.
//
// Positions come from runcheck.FindCommands so they map back to the
// Dockerfile source; OS package-manager detection defers to
// facts.RunFacts.InstallCommands + shell.StripPackageVersion for
// version/arch-tolerant package-name comparison.
func findXdebugCommands(
	runFacts *facts.RunFacts,
	shellVariant shell.Variant,
	sm *sourcemap.SourceMap,
) ([]shell.CommandInfo, int) {
	names := make([]string, 0, len(phpExtensionCommandNames)+len(osPackageManagersForPHPSorted))
	names = append(names, phpExtensionCommandNames...)
	names = append(names, osPackageManagersForPHPSorted...)

	cmds, runStartLine := runcheck.FindCommands(runFacts.Run, shellVariant, sm, names...)
	if len(cmds) == 0 {
		return nil, 0
	}

	filtered := make([]shell.CommandInfo, 0, len(cmds))
	for _, cmd := range cmds {
		if phpExtensionReferencesXdebug(cmd) {
			filtered = append(filtered, cmd)
			continue
		}
		if commandInstallsXdebugPackage(cmd, runFacts.InstallCommands) {
			filtered = append(filtered, cmd)
		}
	}
	return filtered, runStartLine
}

// phpExtensionReferencesXdebug checks whether a docker-php-ext-* or pecl
// command installs or enables Xdebug.
func phpExtensionReferencesXdebug(cmd shell.CommandInfo) bool {
	switch cmd.Name {
	case cmdDockerPHPExtInstall, cmdDockerPHPExtEnable:
		return argsContainXdebug(cmd.Args)
	case cmdPecl:
		return cmd.Subcommand == subcommandInstall && argsContainXdebug(cmd.Args)
	default:
		return false
	}
}

// commandInstallsXdebugPackage reports whether a CommandInfo matching an OS
// package manager corresponds to the exact InstallCommand occurrence whose
// package list contains an Xdebug-bearing package. Defers package-name
// normalization to shell.StripPackageVersion via installCommandInstallsXdebug.
func commandInstallsXdebugPackage(cmd shell.CommandInfo, installs []shell.InstallCommand) bool {
	if !osPackageManagersForPHP[strings.ToLower(cmd.Name)] {
		return false
	}
	return slices.ContainsFunc(installs, func(ic shell.InstallCommand) bool {
		return installCommandMatchesCommand(cmd, ic) && installCommandInstallsXdebug(ic)
	})
}

func installCommandMatchesCommand(cmd shell.CommandInfo, ic shell.InstallCommand) bool {
	if !strings.EqualFold(cmd.Name, ic.Manager) {
		return false
	}
	if cmd.Subcommand != ic.Subcommand {
		return false
	}
	if len(ic.Packages) == 0 {
		return false
	}

	packages := make([]string, 0, len(ic.Packages))
	for _, pkg := range ic.Packages {
		packages = append(packages, pkg.Normalized)
	}
	return slices.Equal(packageArgsForPHPManager(cmd.Name, argsAfterSubcommand(cmd.Args, cmd.Subcommand)), packages)
}

func packageArgsForPHPManager(manager string, args []string) []string {
	var got []string
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if phpManagerFlagConsumesValue(manager, arg) {
				skipNext = true
			}
			continue
		}
		got = append(got, arg)
	}
	return got
}

func phpManagerFlagConsumesValue(manager, flag string) bool {
	switch strings.ToLower(manager) {
	case "apt", "apt-get":
		switch flag {
		case "-o", "--option", "-t", "--target-release":
			return true
		}
	case "dnf", "microdnf", "yum":
		switch flag {
		case "--root", "--installroot", "--releasever", "--repo":
			return true
		}
	}
	return false
}

// installCommandInstallsXdebug reports whether a normalized package-manager
// install references an Xdebug package (e.g., php-xdebug, php8.3-xdebug,
// php83-pecl-xdebug). Only OS-level package managers are considered; an
// npm/pip/composer package with "xdebug" in its name does not imply the PHP
// Xdebug extension is installed in the image.
func installCommandInstallsXdebug(ic shell.InstallCommand) bool {
	if !osPackageManagersForPHP[strings.ToLower(ic.Manager)] {
		return false
	}
	return slices.ContainsFunc(ic.Packages, packageNameContainsXdebug)
}

// packagesOnlyXdebug reports whether every package in an install command is
// an Xdebug package. Returns false for empty package lists.
func packagesOnlyXdebug(ic shell.InstallCommand) bool {
	if !osPackageManagersForPHP[strings.ToLower(ic.Manager)] || len(ic.Packages) == 0 {
		return false
	}
	for _, pkg := range ic.Packages {
		if !packageNameContainsXdebug(pkg) {
			return false
		}
	}
	return true
}

func packageNameContainsXdebug(pkg shell.PackageArg) bool {
	name := strings.ToLower(shell.StripPackageVersion(pkg.Normalized))
	return strings.Contains(name, "xdebug")
}

// argsContainXdebug checks if any non-flag arg is "xdebug" or starts with
// "xdebug-" (used by docker-php-ext-* and `pecl install`, where args are
// PHP extension names, not distro package names).
func argsContainXdebug(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		lower := strings.ToLower(arg)
		if lower == "xdebug" || strings.HasPrefix(lower, "xdebug-") {
			return true
		}
	}
	return false
}
