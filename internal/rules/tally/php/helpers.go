package php

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/runcheck"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

const subcommandInstall = "install"

var nonProductionStageTokens = []string{"dev", "development", "test", "testing", "ci", "debug"}

func stageLooksLikeDev(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}

	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		switch r {
		case '-', '_', '.', '/', ':':
			return true
		default:
			return false
		}
	})
	if len(parts) == 0 {
		parts = []string{normalized}
	}

	return slices.ContainsFunc(parts, func(part string) bool {
		return slices.Contains(nonProductionStageTokens, part)
	})
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

// xdebugCommandNames are the command names that can install or enable Xdebug.
var xdebugCommandNames = []string{
	"docker-php-ext-install",
	"docker-php-ext-enable",
	"pecl",
	"apt-get", "apt",
	"apk",
	"dnf", "yum",
}

// findXdebugCommands returns commands that install or enable Xdebug in a RUN instruction.
func findXdebugCommands(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	sm *sourcemap.SourceMap,
) ([]shell.CommandInfo, int) {
	cmds, runStartLine := runcheck.FindCommands(run, shellVariant, sm, xdebugCommandNames...)
	if len(cmds) == 0 {
		return nil, 0
	}

	filtered := make([]shell.CommandInfo, 0, len(cmds))
	for _, cmd := range cmds {
		if commandReferencesXdebug(cmd) {
			filtered = append(filtered, cmd)
		}
	}
	return filtered, runStartLine
}

// commandReferencesXdebug checks whether a parsed command installs or enables Xdebug.
func commandReferencesXdebug(cmd shell.CommandInfo) bool {
	switch cmd.Name {
	case "docker-php-ext-install", "docker-php-ext-enable":
		return argsContainXdebug(cmd.Args)
	case "pecl":
		return cmd.Subcommand == subcommandInstall && argsContainXdebug(cmd.Args)
	case "apt-get", "apt":
		return cmd.Subcommand == subcommandInstall && argsContainXdebugSubstring(cmd.Args)
	case "apk":
		return cmd.Subcommand == command.Add && argsContainXdebugSubstring(cmd.Args)
	case "dnf", "yum":
		return cmd.Subcommand == subcommandInstall && argsContainXdebugSubstring(cmd.Args)
	default:
		return false
	}
}

// argsContainXdebug checks if any non-flag arg is "xdebug" or starts with "xdebug-" (versioned pecl).
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

// argsContainXdebugSubstring checks if any non-flag arg contains "xdebug" as a substring.
// Used for package-manager installs where package names vary (php-xdebug, php8.3-xdebug, etc.).
func argsContainXdebugSubstring(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.Contains(strings.ToLower(arg), "xdebug") {
			return true
		}
	}
	return false
}
