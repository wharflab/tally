package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
)

type downloadConfigTrigger int

const (
	downloadConfigTriggerNone downloadConfigTrigger = iota
	downloadConfigTriggerInstall
	downloadConfigTriggerInvocation
)

type downloadConfigStageContext struct {
	file             string
	meta             rules.RuleMetadata
	stageEnvKey      string
	envAssignment    string
	destination      string
	comment          string
	violationMessage string
	violationDetail  string
	fixDescription   string
	content          string
	isWindows        bool
	hasConfig        func(*facts.StageFacts) bool
	triggerKind      func(*facts.RunFacts, bool) downloadConfigTrigger
	skipInvocation   func(*facts.RunFacts) bool
}

type downloadConfigRuleSpec struct {
	stageEnvKey                   string
	linuxEnvValue                 string
	windowsEnvValue               string
	destination                   string
	comment                       string
	violationMessage              string
	violationDetail               string
	fixDescription                string
	content                       string
	hasConfig                     func(*facts.StageFacts) bool
	triggerKind                   func(*facts.RunFacts, bool) downloadConfigTrigger
	skipAddUnpackOwnedInvocations bool
}

func makeDownloadConfigStageContextBuilder(
	input rules.LintInput,
	meta rules.RuleMetadata,
	spec downloadConfigRuleSpec,
) func(*facts.StageFacts) *downloadConfigStageContext {
	preferAddUnpackEnabled := spec.skipAddUnpackOwnedInvocations &&
		input.IsRuleEnabled(PreferAddUnpackRuleCode)

	return func(stageFacts *facts.StageFacts) *downloadConfigStageContext {
		if stageFacts == nil {
			return nil
		}

		isWindows := stageFacts.BaseImageOS == semantic.BaseImageOSWindows
		envValue := spec.linuxEnvValue
		if isWindows && spec.windowsEnvValue != "" {
			envValue = spec.windowsEnvValue
		}

		return &downloadConfigStageContext{
			file:             input.File,
			meta:             meta,
			stageEnvKey:      spec.stageEnvKey,
			envAssignment:    spec.stageEnvKey + "=" + envValue,
			destination:      spec.destination,
			comment:          spec.comment,
			violationMessage: spec.violationMessage,
			violationDetail:  spec.violationDetail,
			fixDescription:   spec.fixDescription,
			content:          spec.content,
			isWindows:        isWindows,
			hasConfig:        spec.hasConfig,
			triggerKind:      spec.triggerKind,
			skipInvocation: func(runFacts *facts.RunFacts) bool {
				return preferAddUnpackEnabled &&
					hasRemoteArchiveExtraction(runFacts.CommandScript, runFacts.Shell.Variant)
			},
		}
	}
}

func checkDownloadConfigStages(
	input rules.LintInput,
	makeStageContext func(*facts.StageFacts) *downloadConfigStageContext,
) []rules.Violation {
	if input.Facts == nil {
		return nil
	}

	configuredStages := map[int]bool{}
	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		stageFacts := input.Facts.Stage(stageIdx)
		ctx := makeStageContext(stageFacts)
		if ctx == nil {
			continue
		}

		if parentConfigured(stageIdx, input.Semantic, configuredStages) {
			configuredStages[stageIdx] = true
			continue
		}

		v := checkDownloadConfigStage(stageFacts, stage, ctx)
		if v != nil {
			configuredStages[stageIdx] = true
			violations = append(violations, *v)
			continue
		}

		if stageHasDownloadConfig(stageFacts, ctx) {
			configuredStages[stageIdx] = true
		}
	}

	return violations
}

// parentConfigured returns true if this stage's base image is a local stage
// reference that already has (or will have) the download config.
func parentConfigured(stageIdx int, sem *semantic.Model, configured map[int]bool) bool {
	if sem == nil {
		return false
	}
	info := sem.StageInfo(stageIdx)
	if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef {
		return false
	}
	return configured[info.BaseImage.StageIndex]
}

func stageHasDownloadConfig(stageFacts *facts.StageFacts, ctx *downloadConfigStageContext) bool {
	if stageFacts == nil || ctx == nil {
		return false
	}
	if ctx.stageEnvKey != "" && stageFacts.EffectiveEnv.Values[ctx.stageEnvKey] != "" {
		return true
	}
	if ctx.hasConfig == nil {
		return false
	}
	return ctx.hasConfig(stageFacts)
}

func checkDownloadConfigStage(
	stageFacts *facts.StageFacts,
	stage instructions.Stage,
	ctx *downloadConfigStageContext,
) *rules.Violation {
	if stageFacts == nil {
		return nil
	}
	if stageHasDownloadConfig(stageFacts, ctx) {
		return nil
	}

	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil || !runFacts.UsesShell {
			continue
		}

		switch ctx.triggerKind(runFacts, ctx.isWindows) {
		case downloadConfigTriggerNone:
			continue
		case downloadConfigTriggerInstall:
			return buildDownloadConfigViolation(runFacts.Run, runFacts.Run, ctx)
		case downloadConfigTriggerInvocation:
			if ctx.skipInvocation != nil && ctx.skipInvocation(runFacts) {
				continue
			}
			firstRun := firstRunInStage(stage)
			if firstRun == nil {
				firstRun = runFacts.Run
			}
			return buildDownloadConfigViolation(runFacts.Run, firstRun, ctx)
		}
	}

	return nil
}

// firstRunInStage returns the first RunCommand in a stage, or nil.
func firstRunInStage(stage instructions.Stage) *instructions.RunCommand {
	for _, cmd := range stage.Commands {
		if run, ok := cmd.(*instructions.RunCommand); ok {
			return run
		}
	}
	return nil
}

func buildDownloadConfigViolation(
	violationRun, insertBeforeRun *instructions.RunCommand,
	ctx *downloadConfigStageContext,
) *rules.Violation {
	loc := rules.NewLocationFromRanges(ctx.file, violationRun.Location())
	v := rules.NewViolation(
		loc,
		ctx.meta.Code,
		ctx.violationMessage,
		ctx.meta.DefaultSeverity,
	).WithDocURL(ctx.meta.DocURL).WithDetail(ctx.violationDetail)

	if fix := buildDownloadConfigFix(insertBeforeRun, ctx); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

func buildDownloadConfigFix(
	insertBeforeRun *instructions.RunCommand,
	ctx *downloadConfigStageContext,
) *rules.SuggestedFix {
	runLoc := insertBeforeRun.Location()
	if len(runLoc) == 0 {
		return nil
	}

	insertLine := runLoc[0].Start.Line
	insertCol := runLoc[0].Start.Character
	copyHeredoc := buildDownloadConfigCopyHeredoc(ctx.content, ctx.destination, ctx.isWindows)
	newText := fmt.Sprintf("%s\nENV %s\n%s\n", ctx.comment, ctx.envAssignment, copyHeredoc)

	return &rules.SuggestedFix{
		Description: ctx.fixDescription,
		Safety:      rules.FixSuggestion,
		Priority:    ctx.meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(ctx.file, insertLine, insertCol, insertLine, insertCol),
			NewText:  newText,
		}},
	}
}

func buildDownloadConfigCopyHeredoc(content, destination string, isWindows bool) string {
	var sb strings.Builder
	sb.WriteString("COPY ")
	if !isWindows {
		sb.WriteString("--chmod=0644 ")
	}
	sb.WriteString("<<EOF ")
	sb.WriteString(destination)
	sb.WriteString("\n")
	sb.WriteString(strings.TrimSuffix(content, "\n"))
	sb.WriteString("\nEOF")
	return sb.String()
}
