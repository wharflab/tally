package powershell

import (
	"context"
	"strings"
	"time"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/highlight/extract"
	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	shellutil "github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

const (
	// PowerShellRuleCode enables PowerShell script analysis for Dockerfile snippets.
	PowerShellRuleCode = rules.PowerShellRulePrefix + "PowerShell"

	metaFailureRuleCode = rules.PowerShellRulePrefix + "PowerShellInternalError"

	analyzerRunTimeout = 5 * time.Minute
)

type analyzer interface {
	Analyze(ctx context.Context, req psanalyzer.AnalyzeRequest) ([]psanalyzer.Diagnostic, error)
}

type task struct {
	mapping scriptMapping
}

type runScriptExtractor func(*sourcemap.SourceMap, *parser.Node, rune) (scriptMapping, bool)

// Rule runs PowerShell script analysis on PowerShell snippets in Dockerfile instructions.
type Rule struct {
	analyzer analyzer
}

func NewRule() *Rule {
	return &Rule{analyzer: psanalyzer.NewRunner()}
}

func newRuleWithAnalyzer(a analyzer) *Rule {
	return &Rule{analyzer: a}
}

func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PowerShellRuleCode,
		Name:            "PowerShell",
		Description:     "Runs PowerShell script diagnostics on PowerShell code embedded in Dockerfile instructions",
		DocURL:          rules.TallyDocURL(PowerShellRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "best-practices",
		IsExperimental:  true,
	}
}

func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	if !input.IsRuleEnabled(meta.Code) {
		return nil
	}
	if !input.SlowChecksEnabled {
		return nil
	}

	sm := input.SourceMap()
	if sm == nil || input.AST == nil || input.AST.AST == nil {
		return nil
	}

	tasks := collectTasks(input, sm)
	if len(tasks) == 0 {
		return nil
	}

	settings := analyzerSettings(input.Config)
	violations := make([]rules.Violation, 0, len(tasks))
	for _, task := range tasks {
		violations = append(violations, r.checkMapping(input.File, task.mapping, settings)...)
	}
	return violations
}

func collectTasks(input rules.LintInput, sm *sourcemap.SourceMap) []task {
	nodesByStartLine := extract.NodeIndexFromResult(input.AST)
	escapeToken := input.AST.EscapeToken
	if escapeToken == 0 {
		escapeToken = '\\'
	}

	tasks := make([]task, 0, len(input.Stages))
	for stageIdx, stage := range input.Stages {
		tasks = append(tasks, collectStageTasks(input.Semantic, sm, nodesByStartLine, escapeToken, stageIdx, stage)...)
	}
	return tasks
}

func collectStageTasks(
	sem *semantic.Model,
	sm *sourcemap.SourceMap,
	nodesByStartLine map[int]*parser.Node,
	escapeToken rune,
	stageIdx int,
	stage instructions.Stage,
) []task {
	var info *semantic.StageInfo
	if sem != nil {
		info = sem.StageInfo(stageIdx)
	}

	activeVariant := shellutil.VariantBash
	if info != nil {
		activeVariant = info.ShellSetting.Variant
	}

	tasks := make([]task, 0, len(stage.Commands))
	for _, cmd := range stage.Commands {
		switch c := cmd.(type) {
		case *instructions.ShellCommand:
			activeVariant = shellutil.VariantFromShellCmd(c.Shell)
		case *instructions.RunCommand:
			startLine := extract.CommandStartLine(c.Location())
			variant := activeVariant
			if info != nil {
				variant = info.ShellVariantAtLine(startLine)
			}
			tasks = append(tasks, collectRunCommandTasks(
				sm,
				nodesByStartLine,
				escapeToken,
				c,
				startLine,
				variant,
				extractRunScript,
			)...)
		}
	}

	if info != nil {
		for _, ob := range info.OnbuildInstructions {
			run, ok := ob.Command.(*instructions.RunCommand)
			if !ok {
				continue
			}
			tasks = append(tasks, collectRunCommandTasks(
				sm,
				nodesByStartLine,
				escapeToken,
				run,
				ob.SourceLine,
				info.ShellVariantAtLine(ob.SourceLine),
				extractOnbuildRunScript,
			)...)
		}
	}

	return tasks
}

func collectRunCommandTasks(
	sm *sourcemap.SourceMap,
	nodesByStartLine map[int]*parser.Node,
	escapeToken rune,
	run *instructions.RunCommand,
	startLine int,
	variant shellutil.Variant,
	extractor runScriptExtractor,
) []task {
	if len(run.CmdLine) == 0 {
		return nil
	}

	if !run.PrependShell {
		invocation, ok := parseExecFormPowerShellInvocation(run.CmdLine)
		if !ok {
			return nil
		}
		return []task{{mapping: scriptMapping{
			Script:          invocation.script,
			OriginStartLine: startLine,
			FallbackLine:    startLine,
		}}}
	}

	mapping, ok := extractor(sm, nodesByStartLine[startLine], escapeToken)
	if !ok {
		mapping = scriptMapping{
			Script:          getShellFormScript(run),
			OriginStartLine: startLine,
			FallbackLine:    startLine,
		}
	}
	if strings.TrimSpace(mapping.Script) == "" {
		return nil
	}
	if mapping.ShellNameOverride != "" {
		variant = shellutil.VariantFromShell(mapping.ShellNameOverride)
	}

	if variant.IsPowerShell() {
		return []task{{mapping: mapping}}
	}

	invocation, ok := parseExplicitPowerShellInvocation(mapping.Script)
	if !ok {
		return nil
	}

	invocationLine := mapping.OriginStartLine + invocation.startLine
	return []task{{mapping: scriptMapping{
		Script:            invocation.script,
		OriginStartLine:   invocationLine,
		OriginStartColumn: invocation.startColumn,
		FallbackLine:      invocationLine,
		CanFix:            mapping.CanFix,
	}}}
}

func (r *Rule) checkMapping(file string, mapping scriptMapping, settings psanalyzer.Settings) []rules.Violation {
	if strings.TrimSpace(mapping.Script) == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), analyzerRunTimeout)
	defer cancel()

	diagnostics, err := r.analyzer.Analyze(ctx, psanalyzer.AnalyzeRequest{
		ScriptDefinition: mapping.Script,
		Settings:         settings,
	})
	if err != nil {
		loc := rules.NewLineLocation(file, mapping.FallbackLine)
		msg := "failed to run PowerShell analyzer"
		if detail := strings.TrimSpace(err.Error()); detail != "" {
			msg += ": " + detail
		}
		return []rules.Violation{rules.NewViolation(loc, metaFailureRuleCode, msg, rules.SeverityWarning)}
	}

	violations := make([]rules.Violation, 0, len(diagnostics))
	for _, d := range diagnostics {
		if d.RuleName == "" {
			continue
		}
		loc, ok := mapDiagnosticLocation(file, mapping, d)
		if !ok {
			loc = rules.NewLineLocation(file, mapping.FallbackLine)
		}
		v := rules.NewViolation(
			loc,
			rules.PowerShellRulePrefix+d.RuleName,
			d.Message,
			mapSeverity(d.Severity),
		).WithDocURL(rules.PowerShellDiagnosticDocURL(d.RuleName))
		if fixes := buildPowerShellSuggestedFixes(file, mapping, d); len(fixes) > 0 {
			v = v.WithSuggestedFixes(fixes)
		}
		violations = append(violations, v)
	}
	return violations
}

func mapDiagnosticLocation(file string, mapping scriptMapping, d psanalyzer.Diagnostic) (rules.Location, bool) {
	if d.Line == nil || *d.Line <= 0 {
		return rules.Location{}, false
	}

	line := *d.Line
	col := 1
	if d.Column != nil && *d.Column > 0 {
		col = *d.Column
	}

	endLine := line
	if d.EndLine != nil && *d.EndLine > 0 {
		endLine = *d.EndLine
	}

	endCol := col + 1
	if d.EndColumn != nil && *d.EndColumn > 0 {
		endCol = *d.EndColumn
	}

	return mapScriptRange(file, mapping, line, col, endLine, endCol)
}

func buildPowerShellSuggestedFixes(
	file string,
	mapping scriptMapping,
	d psanalyzer.Diagnostic,
) []*rules.SuggestedFix {
	if !mapping.CanFix || len(d.SuggestedCorrections) == 0 {
		return nil
	}

	fixes := make([]*rules.SuggestedFix, 0, len(d.SuggestedCorrections))
	for _, correction := range d.SuggestedCorrections {
		edit, ok := mapSuggestedCorrection(file, mapping, correction)
		if !ok {
			continue
		}
		description := strings.TrimSpace(correction.Description)
		if description == "" {
			description = "Apply PowerShell fix for " + d.RuleName
		}
		fixes = append(fixes, &rules.SuggestedFix{
			Description: description,
			Edits:       []rules.TextEdit{edit},
			Safety:      rules.FixSuggestion,
			IsPreferred: len(fixes) == 0,
			Priority:    0,
		})
	}
	return fixes
}

func mapSuggestedCorrection(
	file string,
	mapping scriptMapping,
	c psanalyzer.SuggestedCorrection,
) (rules.TextEdit, bool) {
	if c.Line <= 0 || c.Column <= 0 || c.EndLine <= 0 || c.EndColumn <= 0 {
		return rules.TextEdit{}, false
	}
	loc, ok := mapScriptRange(file, mapping, c.Line, c.Column, c.EndLine, c.EndColumn)
	if !ok {
		return rules.TextEdit{}, false
	}
	return rules.TextEdit{
		Location: loc,
		NewText:  c.Text,
	}, true
}

func mapScriptRange(
	file string,
	mapping scriptMapping,
	line int,
	col int,
	endLine int,
	endCol int,
) (rules.Location, bool) {
	if line <= 0 || mapping.OriginStartLine <= 0 {
		return rules.Location{}, false
	}

	startLine := mapping.OriginStartLine + line - 1
	startCol := 0
	if col > 0 {
		startCol = col - 1
	}
	if line == 1 {
		startCol += mapping.OriginStartColumn
	}

	if endLine <= 0 {
		endLine = line
	}
	endDockerLine := mapping.OriginStartLine + endLine - 1

	endDockerCol := startCol + 1
	if endCol > 0 {
		endDockerCol = endCol - 1
		if endLine == 1 {
			endDockerCol += mapping.OriginStartColumn
		}
	}

	if endDockerLine < startLine || (endDockerLine == startLine && endDockerCol < startCol) {
		endDockerLine = startLine
		endDockerCol = startCol + 1
	}

	return rules.NewRangeLocation(file, startLine, startCol, endDockerLine, endDockerCol), true
}

func mapSeverity(sev int) rules.Severity {
	switch sev {
	case 2, 3:
		return rules.SeverityError
	case 1:
		return rules.SeverityWarning
	case 0:
		return rules.SeverityInfo
	default:
		return rules.SeverityWarning
	}
}

func getShellFormScript(run *instructions.RunCommand) string {
	if len(run.Files) > 0 && run.Files[0].Data != "" {
		return run.Files[0].Data
	}
	if len(run.CmdLine) > 0 {
		return strings.Join(run.CmdLine, " ")
	}
	return ""
}

func init() {
	rules.Register(NewRule())
}

var _ analyzer = (*psanalyzer.Runner)(nil)
