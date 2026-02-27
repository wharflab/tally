package shellcheck

import (
	"context"
	"fmt"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/directive"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	intshellcheck "github.com/wharflab/tally/internal/shellcheck"
	"github.com/wharflab/tally/internal/sourcemap"

	"github.com/wharflab/tally/internal/rules"
	"mvdan.cc/sh/v3/syntax"
)

const (
	// ShellCheckRuleCode enables embedded ShellCheck linting.
	ShellCheckRuleCode = rules.ShellcheckRulePrefix + "ShellCheck"

	// shellcheckRunTimeout bounds embedded shellcheck execution per snippet.
	shellcheckRunTimeout = 2 * time.Minute

	shellDialectBash = "bash"
	shellDialectKsh  = "ksh"
)

var defaultProxyEnv = []string{
	"HTTP_PROXY", "http_proxy",
	"HTTPS_PROXY", "https_proxy",
	"FTP_PROXY", "ftp_proxy",
	"NO_PROXY", "no_proxy",
}

var hadolintExcludedCodes = map[int]struct{}{
	2187: {},
	1090: {},
	1091: {},
}

type task struct {
	idx int
	fn  func() []rules.Violation
}

type taskAppender struct {
	tasks []task
}

func (a *taskAppender) add(fn func() []rules.Violation) {
	a.tasks = append(a.tasks, task{idx: len(a.tasks), fn: fn})
}

type collectTasksContext struct {
	app *taskAppender

	input rules.LintInput
	sem   *semantic.Model
	sm    *sourcemap.SourceMap

	nodesByStartLine map[int]*parser.Node
	shellDirectives  []directive.ShellDirective
	escapeToken      rune
}

// Rule runs embedded ShellCheck on shell snippets in Dockerfile instructions.
type Rule struct {
	runner *intshellcheck.Runner
}

func NewRule() *Rule {
	return &Rule{
		runner: intshellcheck.NewRunner(),
	}
}

func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            ShellCheckRuleCode,
		Name:            "ShellCheck",
		Description:     "Runs ShellCheck on shell code embedded in Dockerfile instructions",
		DocURL:          rules.TallyDocURL(ShellCheckRuleCode),
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

	sm := input.SourceMap()
	ast := input.AST
	if sm == nil || ast == nil || ast.AST == nil {
		return nil
	}

	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Safe assertion with nil fallback

	shellDirectives := directive.Parse(sm, nil, nil).ShellDirectives
	nodesByStartLine := buildNodesByStartLine(ast)

	tasks := r.collectTasks(input, sem, sm, nodesByStartLine, shellDirectives, ast.EscapeToken)
	if len(tasks) == 0 {
		return nil
	}

	return runTasks(tasks)
}

func buildNodesByStartLine(ast *parser.Result) map[int]*parser.Node {
	if ast == nil || ast.AST == nil || len(ast.AST.Children) == 0 {
		return nil
	}

	nodes := make(map[int]*parser.Node, len(ast.AST.Children))
	for _, node := range ast.AST.Children {
		if node == nil || node.StartLine <= 0 {
			continue
		}
		nodes[node.StartLine] = node
	}
	return nodes
}

func (r *Rule) collectTasks(
	input rules.LintInput,
	sem *semantic.Model,
	sm *sourcemap.SourceMap,
	nodesByStartLine map[int]*parser.Node,
	shellDirectives []directive.ShellDirective,
	escapeToken rune,
) []task {
	ctx := collectTasksContext{
		app: &taskAppender{},

		input: input,
		sem:   sem,
		sm:    sm,

		nodesByStartLine: nodesByStartLine,
		shellDirectives:  shellDirectives,
		escapeToken:      escapeToken,
	}

	for stageIdx, stage := range input.Stages {
		r.collectTasksForStage(&ctx, stageIdx, stage)
	}

	return ctx.app.tasks
}

func (r *Rule) collectTasksForStage(
	ctx *collectTasksContext,
	stageIdx int,
	stage instructions.Stage,
) {
	stageInfo := (*semantic.StageInfo)(nil)
	if ctx.sem != nil {
		stageInfo = ctx.sem.StageInfo(stageIdx)
	}

	knownEnv := collectKnownEnv(stageInfo)

	stageShellName := initialShellNameForStage(stage, ctx.shellDirectives)
	shellName := stageShellName

	// Track shell state at instruction start lines so ONBUILD triggers can
	// reuse the dialect that was active at the ONBUILD declaration site.
	shellNameByLine := make(map[int]string)

	for _, cmd := range stage.Commands {
		startLine := commandStartLine(cmd.Location())
		if startLine > 0 {
			shellNameByLine[startLine] = shellName
		}

		if shellCmd, ok := cmd.(*instructions.ShellCommand); ok {
			if len(shellCmd.Shell) > 0 && shellCmd.Shell[0] != "" {
				shellName = shellCmd.Shell[0]
			}
			continue
		}

		switch c := cmd.(type) {
		case *instructions.RunCommand:
			r.addRunCommandTask(ctx, c, startLine, shellName, knownEnv)

		case *instructions.CmdCommand:
			if !c.PrependShell {
				continue
			}
			r.addShellFormInstructionTask(ctx, c.Location(), startLine, shellName, knownEnv, command.Cmd, c.CmdLine)

		case *instructions.EntrypointCommand:
			if !c.PrependShell {
				continue
			}
			r.addShellFormInstructionTask(
				ctx,
				c.Location(),
				startLine,
				shellName,
				knownEnv,
				command.Entrypoint,
				c.CmdLine,
			)

		case *instructions.HealthCheckCommand:
			r.addHealthcheckCmdShellTask(ctx, c, startLine, shellName, knownEnv)
		}
	}

	r.collectOnbuildRunTasksForStage(ctx, stageIdx, stageShellName, shellNameByLine, knownEnv)
}

func (r *Rule) collectOnbuildRunTasksForStage(
	ctx *collectTasksContext,
	stageIdx int,
	stageShellName string,
	shellNameByLine map[int]string,
	knownEnv []string,
) {
	if ctx.sem == nil {
		return
	}

	for _, ob := range ctx.sem.OnbuildInstructions(stageIdx) {
		run, ok := ob.Command.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			continue
		}

		shellAtOnbuild := shellNameByLine[ob.SourceLine]
		if shellAtOnbuild == "" {
			shellAtOnbuild = stageShellName
		}

		node := ctx.nodesByStartLine[ob.SourceLine]
		mapping, ok := extractOnbuildRunScript(ctx.sm, node, ctx.escapeToken)
		if !ok {
			// Fall back to Hadolint-parity behavior (report on instruction line only).
			r.addShellSnippetTask(ctx.app, ctx.input.File, run.Location(), shellAtOnbuild, knownEnv, getShellFormScript(run))
			continue
		}

		fallbackLoc := rules.NewLocationFromRanges(ctx.input.File, run.Location())
		r.addShellMappingTask(ctx.app, ctx.input.File, fallbackLoc, shellAtOnbuild, knownEnv, mapping)
	}
}

func (r *Rule) addRunCommandTask(
	ctx *collectTasksContext,
	run *instructions.RunCommand,
	startLine int,
	shellName string,
	knownEnv []string,
) {
	if !run.PrependShell {
		return
	}

	node := ctx.nodesByStartLine[startLine]
	mapping, ok := extractRunScript(ctx.sm, node, ctx.escapeToken)
	if !ok {
		// Fall back to Hadolint-parity behavior (report on instruction line only).
		r.addShellSnippetTask(ctx.app, ctx.input.File, run.Location(), shellName, knownEnv, getShellFormScript(run))
		return
	}

	fallbackLoc := rules.NewLocationFromRanges(ctx.input.File, run.Location())
	r.addShellMappingTask(ctx.app, ctx.input.File, fallbackLoc, shellName, knownEnv, mapping)
}

func (r *Rule) addShellFormInstructionTask(
	ctx *collectTasksContext,
	location []parser.Range,
	startLine int,
	shellName string,
	knownEnv []string,
	keyword string,
	cmdLine []string,
) {
	node := ctx.nodesByStartLine[startLine]
	mapping, ok := extractShellFormScript(ctx.sm, node, ctx.escapeToken, keyword)
	if !ok {
		r.addShellSnippetTask(ctx.app, ctx.input.File, location, shellName, knownEnv, strings.Join(cmdLine, " "))
		return
	}

	fallbackLoc := rules.NewLocationFromRanges(ctx.input.File, location)
	r.addShellMappingTask(ctx.app, ctx.input.File, fallbackLoc, shellName, knownEnv, mapping)
}

func (r *Rule) addHealthcheckCmdShellTask(
	ctx *collectTasksContext,
	hc *instructions.HealthCheckCommand,
	startLine int,
	shellName string,
	knownEnv []string,
) {
	if hc.Health == nil || len(hc.Health.Test) == 0 || hc.Health.Test[0] != "CMD-SHELL" {
		return
	}

	node := ctx.nodesByStartLine[startLine]
	mapping, ok := extractHealthcheckCmdShellScript(ctx.sm, node, ctx.escapeToken)
	if !ok {
		// Best effort fallback: lint only the raw CMD-SHELL body.
		snippet := ""
		if len(hc.Health.Test) > 1 {
			snippet = hc.Health.Test[1]
		}
		r.addShellSnippetTask(ctx.app, ctx.input.File, hc.Location(), shellName, knownEnv, snippet)
		return
	}

	fallbackLoc := rules.NewLocationFromRanges(ctx.input.File, hc.Location())
	r.addShellMappingTask(ctx.app, ctx.input.File, fallbackLoc, shellName, knownEnv, mapping)
}

func (r *Rule) addShellSnippetTask(
	app *taskAppender,
	file string,
	location []parser.Range,
	shellName string,
	knownEnv []string,
	snippet string,
) {
	fileForTask := file
	locationForTask := slices.Clone(location)
	shellNameForTask := shellName
	knownEnvForTask := knownEnv
	snippetForTask := snippet
	app.add(func() []rules.Violation {
		return r.checkShellSnippet(fileForTask, locationForTask, shellNameForTask, knownEnvForTask, snippetForTask)
	})
}

func (r *Rule) addShellMappingTask(
	app *taskAppender,
	file string,
	fallbackLoc rules.Location,
	shellName string,
	knownEnv []string,
	mapping scriptMapping,
) {
	fileForTask := file
	fallbackLocForTask := fallbackLoc
	shellNameForTask := shellName
	knownEnvForTask := knownEnv
	mappingForTask := mapping
	app.add(func() []rules.Violation {
		return r.checkShellMapping(fileForTask, fallbackLocForTask, shellNameForTask, knownEnvForTask, mappingForTask)
	})
}

func runTasks(tasks []task) []rules.Violation {
	workers := min(max(runtime.GOMAXPROCS(0), 1), 4)

	results := make([][]rules.Violation, len(tasks))
	ch := make(chan task)
	var wg sync.WaitGroup
	wg.Add(len(tasks))

	for range workers {
		go func() {
			for t := range ch {
				results[t.idx] = t.fn()
				wg.Done()
			}
		}()
	}

	for _, t := range tasks {
		ch <- t
	}
	close(ch)
	wg.Wait()

	total := 0
	for _, vs := range results {
		total += len(vs)
	}
	violations := make([]rules.Violation, 0, total)
	for _, vs := range results {
		violations = append(violations, vs...)
	}
	return violations
}

func (r *Rule) checkShellSnippet(
	file string,
	location []parser.Range,
	shellName string,
	knownEnv []string,
	snippet string,
) []rules.Violation {
	if strings.TrimSpace(snippet) == "" {
		return nil
	}

	if !isAllowedShebang(snippet) {
		return nil
	}

	dialect, ok := dialectForShellName(shellName)
	if !ok {
		return nil
	}

	prelude, _ := buildPrelude(dialect, knownEnv)
	script := prelude + snippet
	if parseErr := preflightParseShellScript(script, dialect); parseErr != nil {
		return []rules.Violation{rules.NewViolation(
			rules.NewLocationFromRanges(file, location),
			metaParseStatusRuleCode,
			"unable to parse shell script",
			rules.SeverityInfo,
		).WithDetail(parseErr.Error())}
	}

	out, err := r.runShellcheck(script, intshellcheck.Options{
		Dialect:  dialect,
		Severity: "style",
		Norc:     true,
	})
	if err != nil {
		// Tool failures should not hard-fail linting; surface as a single violation
		// on the instruction line once precise mapping lands.
		msg := "failed to run embedded ShellCheck"
		if strings.TrimSpace(err.Error()) != "" {
			msg += ": " + err.Error()
		}
		loc := rules.NewLocationFromRanges(file, location)
		return []rules.Violation{rules.NewViolation(
			loc,
			metaFailureRuleCode,
			msg,
			rules.SeverityWarning,
		)}
	}

	violations := make([]rules.Violation, 0, len(out.Comments))
	baseLoc := rules.NewLocationFromRanges(file, location)
	for _, c := range out.Comments {
		if _, excluded := hadolintExcludedCodes[c.Code]; excluded {
			continue
		}
		ruleCode := rules.ShellcheckRulePrefix + "SC" + fmt.Sprintf("%04d", c.Code)
		sev := mapLevel(c.Level)

		violations = append(violations, rules.NewViolation(
			baseLoc,
			ruleCode,
			c.Message,
			sev,
		).WithDocURL(rules.ShellcheckDocURL("SC"+fmt.Sprintf("%04d", c.Code))))
	}
	return violations
}

func preflightParseShellScript(script, dialect string) error {
	shParser := syntax.NewParser(
		syntax.Variant(shellSyntaxVariantForDialect(dialect)),
		syntax.KeepComments(false),
	)
	_, err := shParser.Parse(strings.NewReader(script), "")
	return err
}

func shellSyntaxVariantForDialect(dialect string) syntax.LangVariant {
	switch dialect {
	case shellDialectBash:
		return syntax.LangBash
	case shellDialectKsh:
		return syntax.LangMirBSDKorn
	default:
		return syntax.LangPOSIX
	}
}

func (r *Rule) checkShellMapping(
	file string,
	fallbackLoc rules.Location,
	shellName string,
	knownEnv []string,
	mapping scriptMapping,
) []rules.Violation {
	if mapping.Script == "" {
		return nil
	}
	if !isAllowedShebang(mapping.Script) {
		return nil
	}

	dialect, ok := dialectForShellName(shellName)
	if !ok {
		return nil
	}

	prelude, headerLines := buildPrelude(dialect, knownEnv)
	script := prelude + mapping.Script

	out, err := r.runShellcheck(script, intshellcheck.Options{
		Dialect:  dialect,
		Severity: "style",
		Norc:     true,
	})
	if err != nil {
		msg := "failed to run embedded ShellCheck"
		if strings.TrimSpace(err.Error()) != "" {
			msg += ": " + err.Error()
		}
		return []rules.Violation{rules.NewViolation(
			fallbackLoc,
			metaFailureRuleCode,
			msg,
			rules.SeverityWarning,
		)}
	}

	violations := make([]rules.Violation, 0, len(out.Comments))
	for _, c := range out.Comments {
		if _, excluded := hadolintExcludedCodes[c.Code]; excluded {
			continue
		}

		ruleCode := rules.ShellcheckRulePrefix + "SC" + fmt.Sprintf("%04d", c.Code)
		sev := mapLevel(c.Level)

		loc, ok := mapShellcheckRange(file, mapping, headerLines, c.Line, c.Column, c.EndLine, c.EndColumn)
		if !ok {
			loc = fallbackLoc
		}

		v := rules.NewViolation(
			loc,
			ruleCode,
			c.Message,
			sev,
		).WithDocURL(rules.ShellcheckDocURL("SC" + fmt.Sprintf("%04d", c.Code)))

		if c.Fix != nil {
			if fix := buildShellcheckSuggestedFix(file, mapping, headerLines, c); fix != nil {
				v = v.WithSuggestedFix(fix)
			}
		}

		violations = append(violations, v)
	}
	return violations
}

func shellcheckRunContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), shellcheckRunTimeout)
}

func (r *Rule) runShellcheck(script string, opts intshellcheck.Options) (intshellcheck.JSON1Output, error) {
	ctx, cancel := shellcheckRunContext()
	defer cancel()
	out, _, err := r.runner.Run(ctx, script, opts)
	return out, err
}

func buildShellcheckSuggestedFix(file string, mapping scriptMapping, headerLines int, c intshellcheck.Comment) *rules.SuggestedFix {
	if c.Fix == nil || len(c.Fix.Replacements) == 0 {
		return nil
	}

	edits := make([]rules.TextEdit, 0, len(c.Fix.Replacements))
	for _, rep := range c.Fix.Replacements {
		edit, ok := mapShellcheckReplacement(file, mapping, headerLines, rep)
		if !ok {
			return nil
		}
		edits = append(edits, edit)
	}

	// Drop fixes that overlap or share the same position. Our fixer has no
	// intra-fix precedence model, and ordering same-position edits is fragile.
	for i := range edits {
		for j := i + 1; j < len(edits); j++ {
			if textEditsOverlap(edits[i], edits[j]) || sameEditSpan(edits[i], edits[j]) {
				return nil
			}
		}
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Apply ShellCheck fix for SC%04d", c.Code),
		Edits:       edits,
		Safety:      rules.FixSuggestion,
		IsPreferred: true,
		Priority:    0,
	}
}

func mapShellcheckReplacement(file string, mapping scriptMapping, headerLines int, rep intshellcheck.Replacement) (rules.TextEdit, bool) {
	loc, ok := mapShellcheckRange(file, mapping, headerLines, rep.Line, rep.Column, rep.EndLine, rep.EndColumn)
	if !ok {
		return rules.TextEdit{}, false
	}

	switch rep.InsertionPoint {
	case "":
		// Replace [start,end).
	case "beforeStart":
		loc.End = loc.Start
	case "afterEnd":
		loc.Start = loc.End
	default:
		return rules.TextEdit{}, false
	}

	return rules.TextEdit{
		Location: loc,
		NewText:  rep.Replacement,
	}, true
}

func mapShellcheckRange(
	file string,
	mapping scriptMapping,
	headerLines int,
	line, col, endLine, endCol int,
) (rules.Location, bool) {
	scriptLine := line - headerLines
	scriptEndLine := endLine - headerLines
	if scriptLine <= 0 || scriptEndLine <= 0 {
		return rules.Location{}, false
	}
	if scriptEndLine < scriptLine {
		scriptEndLine = scriptLine
	}

	startDockerLine := mapping.OriginStartLine + (scriptLine - 1)
	endDockerLine := mapping.OriginStartLine + (scriptEndLine - 1)
	if startDockerLine <= 0 || endDockerLine <= 0 {
		return rules.Location{}, false
	}

	startCol := max(col-1, 0)
	endCol0 := max(endCol-1, 0)
	if endDockerLine < startDockerLine || (endDockerLine == startDockerLine && endCol0 < startCol) {
		endDockerLine = startDockerLine
		endCol0 = startCol
	}

	return rules.NewRangeLocation(file, startDockerLine, startCol, endDockerLine, endCol0), true
}

func sameEditSpan(a, b rules.TextEdit) bool {
	return a.Location.File == b.Location.File &&
		a.Location.Start.Line == b.Location.Start.Line &&
		a.Location.Start.Column == b.Location.Start.Column &&
		a.Location.End.Line == b.Location.End.Line &&
		a.Location.End.Column == b.Location.End.Column
}

func textEditsOverlap(a, b rules.TextEdit) bool {
	// Different files never overlap.
	if a.Location.File != b.Location.File {
		return false
	}

	aStart, aEnd := a.Location.Start, a.Location.End
	bStart, bEnd := b.Location.Start, b.Location.End

	// A is completely before B.
	if aEnd.Line < bStart.Line || (aEnd.Line == bStart.Line && aEnd.Column <= bStart.Column) {
		return false
	}

	// B is completely before A.
	if bEnd.Line < aStart.Line || (bEnd.Line == aStart.Line && bEnd.Column <= aStart.Column) {
		return false
	}

	return true
}

func dialectForShellName(shellName string) (string, bool) {
	variant := shell.VariantFromShell(shellName)
	if !variant.IsShellCheckCompatible() {
		return "", false
	}
	return shellcheckDialect(shellName), true
}

func initialShellNameForStage(stage instructions.Stage, directives []directive.ShellDirective) string {
	shellName := semantic.DefaultShell[0]

	fromLine := -1
	if len(stage.Location) > 0 {
		fromLine = stage.Location[0].Start.Line - 1 // 0-based
	}
	if fromLine < 0 {
		return shellName
	}

	bestLine := -1
	for i := range directives {
		sd := directives[i]
		if sd.Line < fromLine && sd.Line > bestLine {
			bestLine = sd.Line
			shellName = sd.Shell
		}
	}

	return shellName
}

func commandStartLine(location []parser.Range) int {
	if len(location) == 0 {
		return 0
	}
	return location[0].Start.Line
}

func collectKnownEnv(info *semantic.StageInfo) []string {
	seen := make(map[string]struct{})

	seen["PATH"] = struct{}{}
	for _, k := range defaultProxyEnv {
		seen[k] = struct{}{}
	}

	if info != nil && info.Variables != nil {
		for _, a := range info.Variables.Args() {
			if isShellIdent(a.Name) {
				seen[a.Name] = struct{}{}
			}
		}
		for _, e := range info.Variables.Envs() {
			if isShellIdent(e.Name) {
				seen[e.Name] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func buildPrelude(dialect string, envKeys []string) (string, int) {
	var sb strings.Builder

	// Keep a deterministic shebang even though we always pass -s.
	sb.WriteString("#!/bin/sh\n")
	lineCount := 1

	for _, k := range envKeys {
		sb.WriteString("export ")
		sb.WriteString(k)
		sb.WriteString("=1\n")
		lineCount++
	}

	_ = dialect // reserved for future: shell-specific prelude
	return sb.String(), lineCount
}

func getShellFormScript(run *instructions.RunCommand) string {
	// Prefer heredoc content when present.
	if len(run.Files) > 0 && run.Files[0].Data != "" {
		return run.Files[0].Data
	}
	if len(run.CmdLine) > 0 {
		return strings.Join(run.CmdLine, " ")
	}
	return ""
}

func shellcheckDialect(shellName string) string {
	if shellName == "" {
		return "sh"
	}
	name := strings.ToLower(path.Base(strings.ReplaceAll(shellName, `\`, "/")))
	name = strings.TrimSuffix(name, ".exe")

	switch name {
	case "bash", "zsh":
		return shellDialectBash
	case "sh":
		return "sh"
	case "dash":
		return "dash"
	case "ash":
		return "busybox"
	case "ksh", "mksh":
		return shellDialectKsh
	default:
		return "sh"
	}
}

func isAllowedShebang(script string) bool {
	// Hadolint behavior: if the snippet begins with a shebang not in an allowlist,
	// skip ShellCheck entirely.
	line, _, _ := strings.Cut(script, "\n")
	line = strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(line, "#!") {
		return true
	}

	allowed := []string{
		"#!/bin/sh",
		"#!/bin/bash",
		"#!/bin/ksh",
		"#!/usr/bin/env sh",
		"#!/usr/bin/env bash",
		"#!/usr/bin/env ksh",
	}
	return slices.Contains(allowed, line)
}

func isShellIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isIdentStartRune(r) {
				return false
			}
			continue
		}
		if !isIdentContinueRune(r) {
			return false
		}
	}
	return true
}

func isIdentStartRune(r rune) bool {
	return r == '_' || isASCIILetter(r)
}

func isIdentContinueRune(r rune) bool {
	return isIdentStartRune(r) || isASCIIDigit(r)
}

func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func mapLevel(level string) rules.Severity {
	switch strings.ToLower(level) {
	case "error":
		return rules.SeverityError
	case "warning":
		return rules.SeverityWarning
	case "info":
		return rules.SeverityInfo
	case "style":
		return rules.SeverityStyle
	default:
		return rules.SeverityWarning
	}
}

const metaFailureRuleCode = rules.ShellcheckRulePrefix + "ShellCheckInternalError"
const metaParseStatusRuleCode = rules.ShellcheckRulePrefix + "ShellCheckPreflightParseError"

func init() {
	rules.Register(NewRule())
}
