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
		DefaultSeverity: rules.SeverityOff,
		Category:        "best-practices",
		IsExperimental:  true,
	}
}

func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	if !input.IsRuleEnabled(meta.Code) {
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

	violations := make([]rules.Violation, 0, len(tasks))
	for _, task := range tasks {
		violations = append(violations, r.checkMapping(input.File, task.mapping)...)
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
	}}}
}

func (r *Rule) checkMapping(file string, mapping scriptMapping) []rules.Violation {
	if strings.TrimSpace(mapping.Script) == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), analyzerRunTimeout)
	defer cancel()

	diagnostics, err := r.analyzer.Analyze(ctx, psanalyzer.AnalyzeRequest{ScriptDefinition: mapping.Script})
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
		violations = append(violations, rules.NewViolation(
			loc,
			rules.PowerShellRulePrefix+d.RuleName,
			d.Message,
			mapSeverity(d.Severity),
		).WithDocURL(rules.PowerShellDiagnosticDocURL(d.RuleName)))
	}
	return violations
}

func mapDiagnosticLocation(file string, mapping scriptMapping, d psanalyzer.Diagnostic) (rules.Location, bool) {
	if d.Line == nil || *d.Line <= 0 {
		return rules.Location{}, false
	}

	line := *d.Line
	startLine := mapping.OriginStartLine + line - 1
	startCol := 0
	if d.Column != nil && *d.Column > 0 {
		startCol = *d.Column - 1
	}
	if line == 1 {
		startCol += mapping.OriginStartColumn
	}

	endLine := startLine
	if d.EndLine != nil && *d.EndLine > 0 {
		endLine = mapping.OriginStartLine + *d.EndLine - 1
	}

	endCol := startCol + 1
	if d.EndColumn != nil && *d.EndColumn > 0 {
		endCol = *d.EndColumn - 1
		if d.EndLine != nil && *d.EndLine == 1 {
			endCol += mapping.OriginStartColumn
		} else if d.EndLine == nil && line == 1 {
			endCol += mapping.OriginStartColumn
		}
	}

	if endLine < startLine || (endLine == startLine && endCol < startCol) {
		endLine = startLine
		endCol = startCol + 1
	}

	return rules.NewRangeLocation(file, startLine, startCol, endLine, endCol), true
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
