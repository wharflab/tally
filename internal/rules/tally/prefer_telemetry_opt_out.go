package tally

import (
	"fmt"
	"maps"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/sourcemap"
	"github.com/wharflab/tally/internal/telemetry"
)

// PreferTelemetryOptOutRuleCode is the full rule code for prefer-telemetry-opt-out.
const PreferTelemetryOptOutRuleCode = rules.TallyRulePrefix + "prefer-telemetry-opt-out"

const telemetryOptOutComment = "# [tally] settings to opt out from telemetry"

// PreferTelemetryOptOutRule suggests official telemetry opt-out ENV variables
// for stages that clearly use telemetry-enabled developer tools.
type PreferTelemetryOptOutRule struct{}

type telemetryEnvContext struct {
	stage            *instructions.Stage
	fileFacts        *facts.FileFacts
	sem              *semantic.Model
	stageIdx         int
	stageFacts       *facts.StageFacts
	inheritedPlanned map[string]string
	anchorLine       int
}

// NewPreferTelemetryOptOutRule creates a new rule instance.
func NewPreferTelemetryOptOutRule() *PreferTelemetryOptOutRule {
	return &PreferTelemetryOptOutRule{}
}

// Metadata returns the rule metadata.
func (r *PreferTelemetryOptOutRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferTelemetryOptOutRuleCode,
		Name:            "Prefer telemetry opt-out",
		Description:     "Stages using telemetry-enabled tools should set the vendor-documented opt-out environment variables",
		DocURL:          rules.TallyDocURL(PreferTelemetryOptOutRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "privacy",
		IsExperimental:  false,
		FixPriority:     96, // After shell/curl setup fixes, before heredoc transforms.
	}
}

// Check runs the prefer-telemetry-opt-out rule.
func (r *PreferTelemetryOptOutRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	plannedStageEnv := make(map[int]map[string]string, len(input.Stages))
	violations := make([]rules.Violation, 0, len(input.Stages))

	for stageIdx, stage := range input.Stages {
		stageFacts := stageFactsAt(input.Facts, stageIdx)
		semInfo := stageInfoAt(input.Semantic, stageIdx)
		signals := telemetry.DetectStage(stage, stageFacts, semInfo)
		if signals.Empty() {
			continue
		}
		anchor, ok := signals.Anchor()
		if !ok {
			continue
		}

		requiredEnv := requiredTelemetryEnv(signals.OrderedToolIDs())
		inheritedPlanned := inheritedTelemetryEnv(input.Semantic, stageIdx, plannedStageEnv)
		missingEnv := missingTelemetryEnv(telemetryEnvContext{
			stage:            &stage,
			fileFacts:        input.Facts,
			sem:              input.Semantic,
			stageIdx:         stageIdx,
			stageFacts:       stageFacts,
			inheritedPlanned: inheritedPlanned,
			anchorLine:       anchor.Line,
		}, requiredEnv)
		if len(missingEnv) == 0 {
			continue
		}

		missingTools := toolsForMissingEnv(missingEnv)
		if len(missingTools) == 0 {
			continue
		}

		loc := telemetryViolationLocation(input.File, anchor)
		message, detail := telemetryViolationMessage(missingTools)
		v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithDetail(detail)

		if fix := buildTelemetryFix(input.File, sm, &stage, missingTools, meta); fix != nil {
			v = v.WithSuggestedFix(fix)
		}

		plannedStageEnv[stageIdx] = missingEnv
		violations = append(violations, v)
	}

	return violations
}

func stageFactsAt(fileFacts *facts.FileFacts, stageIdx int) *facts.StageFacts {
	if fileFacts == nil {
		return nil
	}
	return fileFacts.Stage(stageIdx)
}

func stageInfoAt(sem *semantic.Model, stageIdx int) *semantic.StageInfo {
	if sem == nil {
		return nil
	}
	return sem.StageInfo(stageIdx)
}

func requiredTelemetryEnv(toolIDs []telemetry.ToolID) map[string]string {
	required := make(map[string]string, len(toolIDs))
	for _, toolID := range toolIDs {
		tool, ok := telemetry.ToolByID(toolID)
		if !ok {
			continue
		}
		required[tool.EnvKey] = tool.EnvValue
	}
	return required
}

func inheritedTelemetryEnv(
	sem *semantic.Model,
	stageIdx int,
	plannedStageEnv map[int]map[string]string,
) map[string]string {
	if sem == nil {
		return nil
	}

	inherited := map[string]string{}
	visited := map[int]bool{}
	current := stageIdx

	for {
		info := sem.StageInfo(current)
		if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
			return inherited
		}
		if visited[current] {
			return inherited
		}
		visited[current] = true

		parentIdx := info.BaseImage.StageIndex
		for key, value := range plannedStageEnv[parentIdx] {
			if _, exists := inherited[key]; !exists {
				inherited[key] = value
			}
		}
		current = parentIdx
	}
}

func missingTelemetryEnv(ctx telemetryEnvContext, required map[string]string) map[string]string {
	if len(required) == 0 {
		return nil
	}

	effectiveEnv := effectiveTelemetryEnvAtAnchor(
		ctx.stage,
		ctx.fileFacts,
		ctx.sem,
		ctx.stageIdx,
		ctx.stageFacts,
		ctx.inheritedPlanned,
		ctx.anchorLine,
	)
	caseInsensitive := ctx.stageFacts != nil && ctx.stageFacts.BaseImageOS == semantic.BaseImageOSWindows

	missing := map[string]string{}
	for key, desired := range required {
		if telemetryEnvMatches(effectiveEnv, key, desired, caseInsensitive) {
			continue
		}
		missing[key] = desired
	}
	return missing
}

func telemetryEnvMatches(values map[string]string, key, desired string, caseInsensitive bool) bool {
	if len(values) == 0 {
		return false
	}

	actual, ok := lookupTelemetryEnv(values, key, caseInsensitive)
	return ok && strings.EqualFold(actual, desired)
}

func effectiveTelemetryEnvAtAnchor(
	stage *instructions.Stage,
	fileFacts *facts.FileFacts,
	sem *semantic.Model,
	stageIdx int,
	stageFacts *facts.StageFacts,
	inheritedPlanned map[string]string,
	anchorLine int,
) map[string]string {
	caseInsensitive := stageFacts != nil && stageFacts.BaseImageOS == semantic.BaseImageOSWindows
	values := inheritedActualTelemetryEnv(fileFacts, sem, stageIdx)
	if values == nil {
		values = map[string]string{}
	}

	for key, value := range inheritedPlanned {
		if _, ok := lookupTelemetryEnv(values, key, caseInsensitive); ok {
			continue
		}
		setTelemetryEnv(values, key, value, caseInsensitive)
	}

	if stage == nil || anchorLine <= 0 {
		return values
	}

	for _, cmd := range stage.Commands {
		loc := cmd.Location()
		if len(loc) == 0 {
			continue
		}
		startLine := loc[0].Start.Line
		if startLine > anchorLine {
			break
		}

		envCmd, ok := cmd.(*instructions.EnvCommand)
		if !ok {
			continue
		}

		for _, entry := range envCmd.Env {
			setTelemetryEnv(values, entry.Key, entry.Value, caseInsensitive)
		}
	}

	return values
}

func inheritedActualTelemetryEnv(fileFacts *facts.FileFacts, sem *semantic.Model, stageIdx int) map[string]string {
	if fileFacts == nil || sem == nil {
		return nil
	}

	info := sem.StageInfo(stageIdx)
	if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef || info.BaseImage.StageIndex < 0 {
		return nil
	}

	parent := fileFacts.Stage(info.BaseImage.StageIndex)
	if parent == nil || len(parent.EffectiveEnv.Values) == 0 {
		return nil
	}

	inherited := make(map[string]string, len(parent.EffectiveEnv.Values))
	maps.Copy(inherited, parent.EffectiveEnv.Values)
	return inherited
}

func setTelemetryEnv(values map[string]string, key, value string, caseInsensitive bool) {
	if values == nil {
		return
	}
	if !caseInsensitive {
		values[key] = value
		return
	}

	for currentKey := range values {
		if strings.EqualFold(currentKey, key) {
			delete(values, currentKey)
			break
		}
	}
	values[key] = value
}

func lookupTelemetryEnv(values map[string]string, key string, caseInsensitive bool) (string, bool) {
	if !caseInsensitive {
		value, ok := values[key]
		return value, ok
	}

	for currentKey, value := range values {
		if strings.EqualFold(currentKey, key) {
			return value, true
		}
	}
	return "", false
}

func toolsForMissingEnv(missing map[string]string) []telemetry.Tool {
	if len(missing) == 0 {
		return nil
	}

	tools := make([]telemetry.Tool, 0, len(missing))
	for _, tool := range telemetry.OrderedTools() {
		value, ok := missing[tool.EnvKey]
		if !ok || !strings.EqualFold(value, tool.EnvValue) {
			continue
		}
		tools = append(tools, tool)
	}
	return tools
}

func telemetryViolationLocation(file string, anchor telemetry.Signal) rules.Location {
	if anchor.Command != nil {
		return rules.NewLocationFromRanges(file, anchor.Command.Location())
	}
	return rules.NewLineLocation(file, anchor.Line)
}

func telemetryViolationMessage(tools []telemetry.Tool) (string, string) {
	names := make([]string, 0, len(tools))
	envAssignments := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
		envAssignments = append(envAssignments, tool.EnvKey+"="+tool.EnvValue)
	}

	if len(tools) == 1 {
		return fmt.Sprintf(
				"stage uses %s without its documented telemetry opt-out",
				names[0],
			),
			fmt.Sprintf(
				"Add a grouped telemetry opt-out block near the top of the stage with ENV %s for %s.",
				envAssignments[0],
				names[0],
			)
	}

	return "stage uses tools with documented telemetry opt-outs that are not set",
		fmt.Sprintf(
			"Detected %s. The fix inserts a grouped telemetry opt-out block near the top of the stage with ENV %s.",
			strings.Join(names, ", "),
			strings.Join(envAssignments, ", "),
		)
}

func buildTelemetryFix(
	file string,
	sm *sourcemap.SourceMap,
	stage *instructions.Stage,
	tools []telemetry.Tool,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	startLine := telemetryBlockInsertLine(stage, sm)
	if startLine <= 0 {
		return nil
	}

	indent := ""
	if sm != nil {
		indent = leadingWhitespace(sm.Line(startLine - 1))
	}

	assignments := make([]string, 0, len(tools))
	for _, tool := range tools {
		assignments = append(assignments, tool.EnvKey+"="+tool.EnvValue)
	}
	if len(assignments) == 0 {
		return nil
	}

	lines := []string{
		indent + telemetryOptOutComment,
		indent + "ENV " + strings.Join(assignments, " "),
	}

	return &rules.SuggestedFix{
		Description: "Add official telemetry opt-out environment variables",
		Safety:      rules.FixSuggestion,
		Priority:    meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(file, startLine, 0, startLine, 0),
			NewText:  strings.Join(lines, "\n") + "\n",
		}},
	}
}

func telemetryBlockInsertLine(stage *instructions.Stage, sm *sourcemap.SourceMap) int {
	if stage == nil || len(stage.Location) == 0 {
		return 0
	}

	endLine := resolvedLocationEndLine(stage.Location[len(stage.Location)-1].End.Line, sm)
	for _, cmd := range stage.Commands {
		if _, ok := cmd.(*instructions.ArgCommand); !ok {
			break
		}
		loc := cmd.Location()
		if len(loc) == 0 {
			break
		}
		endLine = resolvedLocationEndLine(loc[len(loc)-1].End.Line, sm)
	}

	return endLine + 1
}

func resolvedLocationEndLine(endLine int, sm *sourcemap.SourceMap) int {
	if sm == nil {
		return endLine
	}
	return sm.ResolveEndLine(endLine)
}

func init() {
	rules.Register(NewPreferTelemetryOptOutRule())
}
