package php

import (
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/runcheck"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

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
	return cmd.Subcommand == "install" && cmd.HasFlag("--no-dev")
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

func composerCommandLocation(
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

func effectiveRunShellVariant(semanticValue any, stageIdx int, run *instructions.RunCommand) (shell.Variant, bool) {
	shellVariant := shell.VariantBash

	sem, _ := semanticValue.(*semantic.Model) //nolint:errcheck // nil-safe assertion
	if sem == nil {
		return shellVariant, true
	}

	info := sem.StageInfo(stageIdx)
	if info == nil {
		return shellVariant, true
	}
	if info.IsWindows() {
		return 0, false
	}

	if run != nil && len(run.Location()) > 0 {
		shellVariant = info.ShellVariantAtLine(run.Location()[0].Start.Line)
	} else {
		shellVariant = info.ShellSetting.Variant
	}

	if shellVariant.HasParser() {
		return shellVariant, true
	}
	if run != nil && !run.PrependShell {
		return shell.VariantBash, true
	}
	return 0, false
}
