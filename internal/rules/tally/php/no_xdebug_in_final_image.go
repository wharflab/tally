package php

import (
	"bytes"
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NoXdebugInFinalImageRuleCode is the full rule code.
const NoXdebugInFinalImageRuleCode = rules.TallyRulePrefix + "php/no-xdebug-in-final-image"

// NoXdebugInFinalImageRule flags Xdebug installations in the final image stage.
type NoXdebugInFinalImageRule struct{}

// NewNoXdebugInFinalImageRule creates the rule.
func NewNoXdebugInFinalImageRule() *NoXdebugInFinalImageRule {
	return &NoXdebugInFinalImageRule{}
}

// Metadata returns the rule metadata.
func (r *NoXdebugInFinalImageRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoXdebugInFinalImageRuleCode,
		Name:            "Xdebug should not be installed in the final image",
		Description:     "Final image installs or enables Xdebug, a development-only tool",
		DocURL:          rules.TallyDocURL(NoXdebugInFinalImageRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practices",
		FixPriority:     88, //nolint:mnd // stable priority contract, consistent with companion PHP rules
	}
}

// Check runs the rule.
func (r *NoXdebugInFinalImageRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		if stageLooksLikeDev(stage.Name) {
			continue
		}

		stageFacts := input.Facts.Stage(stageIdx)
		if stageFacts == nil || !stageFacts.IsLast {
			continue
		}
		if stageFacts.BaseImageOS == semantic.BaseImageOSWindows {
			continue
		}

		violations = append(violations, r.checkRunCommands(stageFacts, input.File, input.Source, meta, sm)...)
		violations = append(violations, r.checkObservableFiles(stageFacts, input.File, meta)...)
	}

	return violations
}

func (r *NoXdebugInFinalImageRule) checkRunCommands(
	stageFacts *facts.StageFacts,
	file string,
	source []byte,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
) []rules.Violation {
	var violations []rules.Violation

	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil {
			continue
		}

		shellVariant, ok := factsRunShellVariant(runFacts)
		if !ok {
			continue
		}

		xdebugCmds, runStartLine := findXdebugCommands(runFacts.Run, shellVariant, sm)
		if len(xdebugCmds) == 0 {
			continue
		}

		allXdebug := allCommandsOnlyXdebug(runFacts.CommandInfos)

		for _, cmd := range xdebugCmds {
			v := rules.NewViolation(
				phpCommandLocation(file, runFacts.Run, cmd, runStartLine),
				meta.Code,
				meta.Description,
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL).WithDetail(
				"Xdebug is a development and debugging tool that should not ship in production images. " +
					"Move the Xdebug installation into a dedicated dev or debug build stage.",
			)

			if allXdebug {
				v = v.WithSuggestedFixes(buildXdebugFixes(file, source, runFacts.Run, meta.FixPriority, sm))
			}

			violations = append(violations, v)
		}
	}

	return violations
}

func (r *NoXdebugInFinalImageRule) checkObservableFiles(
	stageFacts *facts.StageFacts,
	file string,
	meta rules.RuleMetadata,
) []rules.Violation {
	var violations []rules.Violation

	for _, observed := range stageFacts.ObservableFiles {
		if observed == nil || observed.Source == facts.ObservableFileSourceRun {
			continue
		}

		content, ok := observed.Content()
		if !ok || content == "" {
			continue
		}

		// Check filename first; fall back to shebang for extensionless scripts
		// like /usr/local/bin/docker-entrypoint.
		if !looksLikeShellScript(observed.Path) && !contentLooksLikeShellScript(content) {
			continue
		}

		cmds := shell.FindCommands(content, shell.VariantBash, xdebugCommandNames...)
		for _, cmd := range cmds {
			if !commandReferencesXdebug(cmd) {
				continue
			}

			v := rules.NewViolation(
				rules.NewLineLocation(file, observed.Line),
				meta.Code,
				"Observable script installs Xdebug in the final image",
				meta.DefaultSeverity,
			).WithDocURL(meta.DocURL).WithDetail(
				"The script at " + observed.Path + " installs Xdebug. " +
					"Move the Xdebug installation into a dedicated dev or debug build stage.",
			)
			violations = append(violations, v)
		}
	}

	return violations
}

// allCommandsOnlyXdebug reports whether every command in a RUN instruction
// exclusively does Xdebug work (no other extensions or packages), meaning the
// entire instruction can be safely commented out or deleted.
func allCommandsOnlyXdebug(cmds []shell.CommandInfo) bool {
	if len(cmds) == 0 {
		return false
	}
	for _, cmd := range cmds {
		if !commandOnlyDoesXdebug(cmd) {
			return false
		}
	}
	return true
}

// commandOnlyDoesXdebug checks whether a command installs/enables Xdebug
// exclusively, with no other extensions or packages.
func commandOnlyDoesXdebug(cmd shell.CommandInfo) bool {
	switch cmd.Name {
	case "docker-php-ext-install", "docker-php-ext-enable":
		return allNonFlagArgsAreXdebug(cmd.Args)
	case "pecl":
		if cmd.Subcommand != subcommandInstall {
			return false
		}
		return allNonFlagArgsAreXdebug(argsAfterSubcommand(cmd.Args, subcommandInstall))
	case "apt-get", "apt":
		if cmd.Subcommand != subcommandInstall {
			return false
		}
		return allNonFlagArgsAreXdebugSubstring(argsAfterSubcommand(cmd.Args, subcommandInstall))
	case "apk":
		if cmd.Subcommand != subcommandAdd {
			return false
		}
		return allNonFlagArgsAreXdebugSubstring(argsAfterSubcommand(cmd.Args, subcommandAdd))
	case "dnf", "yum":
		if cmd.Subcommand != subcommandInstall {
			return false
		}
		return allNonFlagArgsAreXdebugSubstring(argsAfterSubcommand(cmd.Args, subcommandInstall))
	default:
		return false
	}
}

func allNonFlagArgsAreXdebug(args []string) bool {
	found := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		lower := strings.ToLower(arg)
		if lower != "xdebug" && !strings.HasPrefix(lower, "xdebug-") {
			return false
		}
		found = true
	}
	return found
}

// allNonFlagArgsAreXdebugSubstring is like allNonFlagArgsAreXdebug but uses
// substring matching for package-manager packages (e.g., "php-xdebug").
func allNonFlagArgsAreXdebugSubstring(args []string) bool {
	found := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !strings.Contains(strings.ToLower(arg), "xdebug") {
			return false
		}
		found = true
	}
	return found
}

func argsAfterSubcommand(args []string, subcmd string) []string {
	for i, arg := range args {
		if arg == subcmd {
			return args[i+1:]
		}
	}
	return args
}

// buildXdebugFixes returns comment-out and delete fix alternatives for a RUN
// instruction that only installs/enables Xdebug.
func buildXdebugFixes(
	file string,
	source []byte,
	run *instructions.RunCommand,
	priority int,
	sm *sourcemap.SourceMap,
) []*rules.SuggestedFix {
	locs := run.Location()
	if len(locs) == 0 || sm == nil {
		return nil
	}

	startLine := locs[0].Start.Line // 1-based
	endLine := sm.ResolveEndLine(
		locs[len(locs)-1].End.Line,
	) // 1-based, resolve continuations

	// Build the commented-out text: prefix each source line with "# ".
	var commented strings.Builder
	for l := startLine; l <= endLine; l++ {
		line := sm.Line(l - 1)
		commented.WriteString("# ")
		commented.WriteString(line)
		if l < endLine {
			commented.WriteByte('\n')
		}
	}

	lastLineText := sm.Line(endLine - 1)
	editLoc := rules.NewRangeLocation(file, startLine, 0, endLine, len(lastLineText))

	totalLines := bytes.Count(source, []byte("\n")) + 1
	deleteLoc := rules.DeleteLineLocation(file, startLine, len(lastLineText), totalLines)
	if startLine < endLine {
		// Multi-line: delete from start of first line to start of line after last.
		if endLine < totalLines {
			deleteLoc = rules.NewRangeLocation(file, startLine, 0, endLine+1, 0)
		} else {
			deleteLoc = rules.NewRangeLocation(file, startLine, 0, endLine, len(lastLineText))
		}
	}

	return []*rules.SuggestedFix{
		{
			Description: "Comment out Xdebug installation",
			Safety:      rules.FixSuggestion,
			Priority:    priority,
			IsPreferred: true,
			Edits:       []rules.TextEdit{{Location: editLoc, NewText: commented.String()}},
		},
		{
			Description: "Delete Xdebug installation",
			Safety:      rules.FixUnsafe,
			Priority:    priority,
			Edits:       []rules.TextEdit{{Location: deleteLoc, NewText: ""}},
		},
	}
}

// looksLikeShellScript checks if a file path looks like a shell script.
func looksLikeShellScript(filePath string) bool {
	switch path.Ext(filePath) {
	case ".sh", ".bash":
		return true
	}
	base := strings.ToLower(path.Base(filePath))
	return strings.Contains(base, "entrypoint") || //nolint:customlint // filename pattern, not Dockerfile instruction
		strings.Contains(base, "install") ||
		strings.Contains(base, "setup") ||
		strings.Contains(base, "init") ||
		strings.Contains(base, "start")
}

// contentLooksLikeShellScript checks if file content starts with a shell shebang.
func contentLooksLikeShellScript(content string) bool {
	return strings.HasPrefix(content, "#!/bin/sh") ||
		strings.HasPrefix(content, "#!/bin/bash") ||
		strings.HasPrefix(content, "#!/usr/bin/env sh") ||
		strings.HasPrefix(content, "#!/usr/bin/env bash")
}

func init() {
	rules.Register(NewNoXdebugInFinalImageRule())
}
