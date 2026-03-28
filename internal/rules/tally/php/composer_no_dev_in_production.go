package php

import (
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/runcheck"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// ComposerNoDevInProductionRuleCode is the full rule code.
const ComposerNoDevInProductionRuleCode = rules.TallyRulePrefix + "php/composer-no-dev-in-production"

// ComposerNoDevInProductionRule requires --no-dev for production-like composer installs.
type ComposerNoDevInProductionRule struct{}

// NewComposerNoDevInProductionRule creates the rule.
func NewComposerNoDevInProductionRule() *ComposerNoDevInProductionRule {
	return &ComposerNoDevInProductionRule{}
}

// Metadata returns the rule metadata.
func (r *ComposerNoDevInProductionRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            ComposerNoDevInProductionRuleCode,
		Name:            "Production Composer installs should exclude dev dependencies",
		Description:     "Production Composer install commands should include --no-dev",
		DocURL:          rules.TallyDocURL(ComposerNoDevInProductionRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "security",
		FixPriority:     88,
	}
}

// Check runs the rule.
func (r *ComposerNoDevInProductionRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()
	fileFacts, _ := input.Facts.(*facts.FileFacts) //nolint:errcheck // nil-safe assertion

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		if stageLooksLikeDev(stage.Name) {
			continue
		}

		if fileFacts != nil {
			if stageFacts := fileFacts.Stage(stageIdx); stageFacts != nil {
				violations = append(violations, r.checkStageWithFacts(stageFacts, input.File, meta, sm)...)
				continue
			}
		}

		violations = append(violations, r.checkStageLegacy(input, stageIdx, stage, meta, sm)...)
	}

	return violations
}

func (r *ComposerNoDevInProductionRule) checkStageWithFacts(
	stageFacts *facts.StageFacts,
	file string,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
) []rules.Violation {
	if stageFacts == nil || stageFacts.BaseImageOS == semantic.BaseImageOSWindows {
		return nil
	}

	var violations []rules.Violation
	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil || composerNoDevEnabled(runFacts.Env) {
			continue
		}

		shellVariant, ok := factsRunShellVariant(runFacts)
		if !ok {
			continue
		}

		violations = append(violations, r.checkRun(runFacts.Run, shellVariant, file, meta, sm)...)
	}

	return violations
}

func factsRunShellVariant(runFacts *facts.RunFacts) (shell.Variant, bool) {
	if runFacts == nil {
		return 0, false
	}
	if runFacts.Shell.HasParser {
		return runFacts.Shell.Variant, true
	}
	if !runFacts.UsesShell {
		return shell.VariantBash, true
	}
	return 0, false
}

func (r *ComposerNoDevInProductionRule) checkStageLegacy(
	input rules.LintInput,
	stageIdx int,
	stage instructions.Stage,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
) []rules.Violation {
	envValues := map[string]string{}
	var violations []rules.Violation

	for _, command := range stage.Commands {
		switch cmd := command.(type) {
		case *instructions.EnvCommand:
			for _, kv := range cmd.Env {
				envValues[kv.Key] = facts.Unquote(kv.Value)
			}
		case *instructions.RunCommand:
			if composerNoDevEnabledValues(envValues) {
				continue
			}

			shellVariant, ok := effectiveRunShellVariant(input.Semantic, stageIdx, cmd)
			if !ok {
				continue
			}

			violations = append(violations, r.checkRun(cmd, shellVariant, input.File, meta, sm)...)
		}
	}

	return violations
}

func (r *ComposerNoDevInProductionRule) checkRun(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	file string,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
) []rules.Violation {
	cmds, runStartLine := findComposerCommands(run, shellVariant, sm, "install")
	if len(cmds) == 0 {
		return nil
	}

	var violations []rules.Violation
	for _, cmd := range cmds {
		if composerInstallHasNoDev(cmd) {
			continue
		}

		v := rules.NewViolation(
			composerCommandLocation(file, run, cmd, runStartLine),
			meta.Code,
			meta.Description,
			meta.DefaultSeverity,
		).WithDocURL(meta.DocURL).WithDetail(
			"Composer installs in production-like stages should exclude require-dev packages. " +
				"Add --no-dev or set COMPOSER_NO_DEV=1 for the stage.",
		)

		if fix := runcheck.BuildInsertAfterSubcommandFix(
			file,
			cmd,
			runStartLine,
			sm,
			runcheck.InsertAfterSubcommandFixOptions{
				Text:        " --no-dev",
				Description: "Add --no-dev to composer install",
				Safety:      rules.FixSuggestion,
				Priority:    meta.FixPriority,
			},
		); fix != nil {
			v = v.WithSuggestedFix(fix)
		}

		violations = append(violations, v)
	}

	return violations
}

func init() {
	rules.Register(NewComposerNoDevInProductionRule())
}
