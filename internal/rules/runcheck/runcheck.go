package runcheck

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// RunCommandCallback checks a RUN command with the given shell variant.
type RunCommandCallback func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation

// ScanRunCommandsWithPOSIXShell walks RUN commands and skips shell-form RUNs in
// non-POSIX shells while still allowing exec-form RUNs through.
func ScanRunCommandsWithPOSIXShell(input rules.LintInput, callback RunCommandCallback) []rules.Violation {
	var violations []rules.Violation

	sem, ok := input.Semantic.(*semantic.Model)
	if !ok {
		sem = nil
	}

	for stageIdx, stage := range input.Stages {
		shellVariant := shell.VariantBash
		isNonPOSIX := false
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				isNonPOSIX = !shellVariant.SupportsPOSIXShellAST()
			}
		}

		for _, cmd := range stage.Commands {
			run, ok := cmd.(*instructions.RunCommand)
			if !ok {
				continue
			}
			if isNonPOSIX && run.PrependShell {
				continue
			}

			effectiveVariant := shellVariant
			if isNonPOSIX {
				effectiveVariant = shell.VariantBash
			}
			violations = append(violations, callback(run, effectiveVariant, input.File)...)
		}

		if sem == nil {
			continue
		}
		for _, onbuild := range sem.OnbuildInstructions(stageIdx) {
			run, ok := onbuild.Command.(*instructions.RunCommand)
			if !ok {
				continue
			}
			if isNonPOSIX && run.PrependShell {
				continue
			}

			effectiveVariant := shellVariant
			if isNonPOSIX {
				effectiveVariant = shell.VariantBash
			}
			violations = append(violations, callback(run, effectiveVariant, input.File)...)
		}
	}

	return violations
}

// CommandFlagRuleConfig defines a common command+flag requirement pattern.
type CommandFlagRuleConfig struct {
	CommandNames    []string
	Subcommands     []string
	HasRequiredFlag func(cmd *shell.CommandInfo) bool
	FixFlag         string
	FixDescription  string
	Detail          string
	FixSafety       rules.FixSafety
}

// FindCommands returns matching commands from a RUN instruction and the
// 1-based source start line when shell-form source positions are available.
func FindCommands(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	sm *sourcemap.SourceMap,
	names ...string,
) ([]shell.CommandInfo, int) {
	if run == nil {
		return nil, 0
	}

	if run.PrependShell && sm != nil {
		script, startLine := dockerfile.RunSourceScript(run, sm)
		if script == "" {
			return nil, 0
		}
		return shell.FindCommands(script, shellVariant, names...), startLine
	}

	cmdStr := dockerfile.RunCommandString(run)
	if cmdStr == "" {
		return nil, 0
	}
	return shell.FindCommands(cmdStr, shellVariant, names...), 0
}

// BuildInsertAfterSubcommandFix inserts text immediately after a command
// subcommand when source-aware shell positions are available.
type InsertAfterSubcommandFixOptions struct {
	Text        string
	Description string
	Safety      rules.FixSafety
	Priority    int
}

// BuildInsertAfterSubcommandFix inserts text immediately after a command
// subcommand when source-aware shell positions are available.
func BuildInsertAfterSubcommandFix(
	file string,
	cmd shell.CommandInfo,
	runStartLine int,
	sm *sourcemap.SourceMap,
	opts InsertAfterSubcommandFixOptions,
) *rules.SuggestedFix {
	if sm == nil || runStartLine == 0 || cmd.Subcommand == "" {
		return nil
	}

	editLine := runStartLine + cmd.SubcommandLine
	insertCol := cmd.SubcommandEndCol

	lineIdx := editLine - 1
	if lineIdx < 0 || lineIdx >= sm.LineCount() {
		return nil
	}
	sourceLine := sm.Line(lineIdx)
	if insertCol < 0 || insertCol > len(sourceLine) {
		return nil
	}

	return &rules.SuggestedFix{
		Description: opts.Description,
		Safety:      opts.Safety,
		Priority:    opts.Priority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, editLine, insertCol, editLine, insertCol),
			NewText:  opts.Text,
		}},
	}
}

// CheckCommandFlag checks commands that require a specific flag after matching
// a command name and subcommand.
func CheckCommandFlag(input rules.LintInput, meta rules.RuleMetadata, config CommandFlagRuleConfig) []rules.Violation {
	sm := input.SourceMap()

	return ScanRunCommandsWithPOSIXShell(
		input,
		func(run *instructions.RunCommand, shellVariant shell.Variant, file string) []rules.Violation {
			cmds, runStartLine := FindCommands(run, shellVariant, sm, config.CommandNames...)
			if len(cmds) == 0 {
				return nil
			}

			var violations []rules.Violation
			for _, cmd := range cmds {
				if !cmd.HasAnyArg(config.Subcommands...) {
					continue
				}
				if config.HasRequiredFlag(&cmd) {
					continue
				}

				v := rules.NewViolation(
					rules.NewLocationFromRanges(file, run.Location()),
					meta.Code,
					meta.Description,
					meta.DefaultSeverity,
				).WithDocURL(meta.DocURL).WithDetail(config.Detail)

				if fix := BuildInsertAfterSubcommandFix(
					file,
					cmd,
					runStartLine,
					sm,
					InsertAfterSubcommandFixOptions{
						Text:        config.FixFlag,
						Description: config.FixDescription,
						Safety:      config.FixSafety,
						Priority:    meta.FixPriority,
					},
				); fix != nil {
					v = v.WithSuggestedFix(fix)
				}

				violations = append(violations, v)
			}

			return violations
		},
	)
}
