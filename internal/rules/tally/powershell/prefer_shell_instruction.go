package powershell

import (
	jsonv2 "encoding/json/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	shellutil "github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// PreferShellInstructionRuleCode is the full rule code for prefer-shell-instruction.
const PreferShellInstructionRuleCode = rules.TallyRulePrefix + "powershell/prefer-shell-instruction"

const powerShellPrelude = "$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"

var powerShellSafeUnquotedArgPattern = regexp.MustCompile(`^[A-Za-z0-9_./:\\=-]+$`)

var cmdPathVariablePattern = regexp.MustCompile(`(?i)%path%|!path!`)

var embeddedCmdVariablePattern = regexp.MustCompile(`(?i)(%[a-z_][a-z0-9_]*(?::[^%]+)?%|![a-z_][a-z0-9_]*!|%%~?[a-z]|%~?[0-9])`)

var unsafeCmdBuiltinsForPowerShell = map[string]bool{
	"assoc":      true,
	"break":      true,
	"cd":         true,
	"chdir":      true,
	"cls":        true,
	"color":      true,
	command.Copy: true,
	"date":       true,
	"del":        true,
	"dir":        true,
	"echo":       true,
	"erase":      true,
	"for":        true,
	"ftype":      true,
	"goto":       true,
	"if":         true,
	"md":         true,
	"mkdir":      true,
	"mklink":     true,
	"move":       true,
	"path":       true,
	"pause":      true,
	"popd":       true,
	"prompt":     true,
	"pushd":      true,
	"rd":         true,
	"rem":        true,
	"ren":        true,
	"rename":     true,
	"rmdir":      true,
	"set":        true,
	"shift":      true,
	"start":      true,
	"time":       true,
	"title":      true,
	"type":       true,
	"ver":        true,
	"verify":     true,
	"vol":        true,
}

// PreferShellInstructionRule recommends using SHELL for repeated PowerShell RUN wrappers.
type PreferShellInstructionRule struct{}

// NewPreferShellInstructionRule creates a new rule instance.
func NewPreferShellInstructionRule() *PreferShellInstructionRule {
	return &PreferShellInstructionRule{}
}

// Metadata returns the rule metadata.
func (r *PreferShellInstructionRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferShellInstructionRuleCode,
		Name:            "Prefer PowerShell SHELL instruction",
		Description:     "Use a SHELL instruction instead of repeating powershell -Command or pwsh -Command wrappers",
		DocURL:          rules.TallyDocURL(PreferShellInstructionRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  true,
		FixPriority:     95,
	}
}

type explicitPowerShellInvocation struct {
	executable     string
	normalizedExec string
	prefixArgs     []string
	script         string
}

type powerShellRun struct {
	run        *instructions.RunCommand
	invocation explicitPowerShellInvocation
}

type powerShellCluster struct {
	startIdx    int
	shellBefore shellutil.Variant
	runs        []powerShellRun
}

// Check runs the rule.
func (r *PreferShellInstructionRule) Check(input rules.LintInput) []rules.Violation {
	if len(powershellStages(input)) == 0 {
		return nil
	}

	meta := r.Metadata()
	sm := input.SourceMap()

	sem := input.Semantic

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		currentShell := initialStageShellVariant(sem, stageIdx)
		cluster := powerShellCluster{
			startIdx: -1,
			runs:     make([]powerShellRun, 0, 4),
		}

		flush := func() {
			if len(cluster.runs) < 2 {
				cluster.startIdx = -1
				cluster.runs = cluster.runs[:0]
				return
			}
			if v := buildPreferShellViolation(meta, input.File, sm, stageIdx, stage.Commands, cluster); v != nil {
				violations = append(violations, *v)
			}
			cluster.startIdx = -1
			cluster.runs = cluster.runs[:0]
		}

		for cmdIdx, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.ShellCommand:
				flush()
				currentShell = shellutil.VariantFromShellCmd(c.Shell)
			case *instructions.RunCommand:
				if !c.PrependShell {
					continue
				}
				if currentShell.IsPowerShell() {
					flush()
					continue
				}
				if len(c.CmdLine) == 0 {
					flush()
					continue
				}

				invocation, ok := parseExplicitPowerShellInvocation(c.CmdLine[0])
				if !ok {
					flush()
					continue
				}
				if cluster.startIdx < 0 {
					cluster.startIdx = cmdIdx
					cluster.shellBefore = currentShell
				}
				cluster.runs = append(cluster.runs, powerShellRun{
					run:        c,
					invocation: invocation,
				})
			default:
				// Non-RUN instructions do not participate in wrapper clustering.
			}
		}

		flush()
	}

	return violations
}

func initialStageShellVariant(sem *semantic.Model, stageIdx int) shellutil.Variant {
	if sem == nil {
		return shellutil.VariantFromShellCmd(semantic.DefaultShell)
	}
	info := sem.StageInfo(stageIdx)
	if info == nil {
		return shellutil.VariantFromShellCmd(semantic.DefaultShell)
	}
	if info.IsWindows() {
		return shellutil.VariantCmd
	}
	return shellutil.VariantFromShellCmd(semantic.DefaultShell)
}

func buildPreferShellViolation(
	meta rules.RuleMetadata,
	file string,
	sm *sourcemap.SourceMap,
	stageIdx int,
	stageCommands []instructions.Command,
	cluster powerShellCluster,
) *rules.Violation {
	first := cluster.runs[0]
	loc := rules.NewLocationFromRanges(file, first.run.Location())
	if loc.IsFileLevel() {
		return nil
	}

	message := "Prefer a SHELL instruction for repeated PowerShell RUN wrappers"
	detail := "Found " + pluralizeCount(len(cluster.runs), command.Run) + " invoking " + first.invocation.normalizedExec +
		" explicitly before any PowerShell SHELL instruction."

	v := rules.NewViolation(loc, meta.Code, message, meta.DefaultSeverity).WithDocURL(meta.DocURL).WithDetail(detail)
	v.StageIndex = stageIdx

	if fix := buildSuggestedFix(file, sm, stageCommands, cluster); fix != nil {
		v.SuggestedFix = fix
	}

	return &v
}

func pluralizeCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun + " command"
	}
	return strconv.Itoa(n) + " " + noun + " commands"
}

func buildSuggestedFix(
	file string,
	sm *sourcemap.SourceMap,
	stageCommands []instructions.Command,
	cluster powerShellCluster,
) *rules.SuggestedFix {
	if !clusterHasConsistentShell(cluster.runs) {
		return nil
	}

	first := cluster.runs[0]
	if len(first.run.Location()) == 0 {
		return nil
	}

	shellArgs := make([]string, 0, 3+len(first.invocation.prefixArgs))
	shellArgs = append(shellArgs, first.invocation.executable)
	shellArgs = append(shellArgs, first.invocation.prefixArgs...)
	shellArgs = append(shellArgs, "-Command", powerShellPrelude)

	shellJSON, err := jsonv2.Marshal(shellArgs)
	if err != nil {
		return nil
	}

	startLine := first.run.Location()[0].Start.Line
	if sm != nil {
		startLine = sm.EffectiveStartLine(startLine, first.run.Comments())
	}
	indent := ""
	if sm != nil {
		indent = leadingIndent(sm.Line(startLine - 1))
	}

	runEdits, ok := collectStageRunEditsForPowerShell(file, sm, stageCommands, cluster)
	if !ok {
		return nil
	}

	edits := make([]rules.TextEdit, 0, len(runEdits)+1)
	edits = append(edits, rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine, 0, startLine, 0),
		NewText:  indent + "SHELL " + string(shellJSON) + "\n",
	})
	edits = append(edits, runEdits...)

	return &rules.SuggestedFix{
		Description: "Insert a PowerShell SHELL instruction and remove repeated wrappers",
		Safety:      rules.FixSuggestion,
		IsPreferred: true,
		Priority:    95,
		Edits:       edits,
	}
}

func clusterHasConsistentShell(cluster []powerShellRun) bool {
	if len(cluster) < 2 {
		return false
	}

	first := cluster[0].invocation
	for _, item := range cluster[1:] {
		cur := item.invocation
		if !strings.EqualFold(cur.normalizedExec, first.normalizedExec) {
			return false
		}
		if !slices.EqualFunc(cur.prefixArgs, first.prefixArgs, strings.EqualFold) {
			return false
		}
	}

	return true
}

func collectStageRunEditsForPowerShell(
	file string,
	sm *sourcemap.SourceMap,
	stageCommands []instructions.Command,
	cluster powerShellCluster,
) ([]rules.TextEdit, bool) {
	clusterRuns := make(map[*instructions.RunCommand]powerShellRun, len(cluster.runs))
	for _, item := range cluster.runs {
		clusterRuns[item.run] = item
	}

	edits := make([]rules.TextEdit, 0, len(cluster.runs))
	for idx := cluster.startIdx; idx < len(stageCommands); idx++ {
		switch cmd := stageCommands[idx].(type) {
		case *instructions.ShellCommand:
			return edits, true
		case *instructions.CmdCommand:
			if cmd.PrependShell {
				return nil, false
			}
		case *instructions.EntrypointCommand:
			if cmd.PrependShell {
				return nil, false
			}
		case *instructions.HealthCheckCommand:
			if healthcheckUsesShell(cmd) {
				return nil, false
			}
		case *instructions.RunCommand:
			if !cmd.PrependShell {
				continue
			}

			if item, ok := clusterRuns[cmd]; ok {
				edit, ok := buildPowerShellWrapperRewriteEdit(file, sm, item)
				if !ok {
					return nil, false
				}
				edits = append(edits, edit)
				continue
			}

			edit, needed, ok := adaptImpactedRunForPowerShell(file, sm, cluster.shellBefore, cmd)
			if !ok {
				return nil, false
			}
			if needed {
				edits = append(edits, edit)
			}
		}
	}

	return edits, true
}

func healthcheckUsesShell(cmd *instructions.HealthCheckCommand) bool {
	if cmd == nil || cmd.Health == nil || len(cmd.Health.Test) == 0 {
		return false
	}
	return strings.EqualFold(cmd.Health.Test[0], "CMD-SHELL")
}

func adaptImpactedRunForPowerShell(
	file string,
	sm *sourcemap.SourceMap,
	shellBefore shellutil.Variant,
	run *instructions.RunCommand,
) (rules.TextEdit, bool, bool) {
	if len(run.CmdLine) == 0 {
		return rules.TextEdit{}, false, false
	}
	if _, ok := parseExplicitPowerShellInvocation(run.CmdLine[0]); ok {
		return rules.TextEdit{}, false, true
	}
	if shellBefore != shellutil.VariantCmd {
		return rules.TextEdit{}, false, false
	}

	newScript, changed, ok := rewriteCompatibleCmdScriptForPowerShell(run.CmdLine[0])
	if !ok {
		return rules.TextEdit{}, false, false
	}
	if !changed {
		return rules.TextEdit{}, false, true
	}

	edit, ok := buildRunBodyRewriteEdit(file, sm, run, newScript)
	if !ok {
		return rules.TextEdit{}, false, false
	}
	return edit, true, true
}

func rewriteCompatibleCmdScriptForPowerShell(script string) (string, bool, bool) {
	if canPassThroughCmdInvocation(script) {
		return "", false, true
	}

	analysis := shellutil.AnalyzeCmdScript(script)
	if analysis == nil || analysis.HasConditionals || analysis.HasPipes || analysis.HasRedirections || analysis.HasControlFlow {
		return "", false, false
	}
	if len(analysis.Commands) != 1 {
		return "", false, false
	}

	cmd := analysis.Commands[0]
	if unsafeCmdBuiltinsForPowerShell[cmd.Name] {
		return "", false, false
	}

	if rewritten, ok := rewriteSetXPathCommandForPowerShell(cmd); ok {
		return rewritten, true, true
	}

	for _, arg := range cmd.Args {
		if !isPowerShellSafeCmdArg(arg) {
			return "", false, false
		}
	}

	if analysis.HasVariableReferences {
		return "", false, false
	}

	return "", false, true
}

func rewriteSetXPathCommandForPowerShell(cmd shellutil.CommandInfo) (string, bool) {
	if cmd.Name != "setx" {
		return "", false
	}

	args := append([]string(nil), cmd.Args...)
	options := make([]string, 0, 1)
	for len(args) > 0 && strings.HasPrefix(args[0], "/") {
		options = append(options, args[0])
		args = args[1:]
	}
	if len(args) != 2 || !strings.EqualFold(shellutil.DropQuotes(args[0]), "path") {
		return "", false
	}

	value := stripMatchingQuotes(args[1])
	if !cmdPathVariablePattern.MatchString(value) {
		return "", false
	}
	if strings.Contains(value, `"`) || strings.Contains(value, "`") {
		return "", false
	}

	translated := cmdPathVariablePattern.ReplaceAllStringFunc(value, func(match string) string {
		return "$env:Path"
	})
	if hasCmdVariableSyntax(translated) {
		return "", false
	}

	parts := make([]string, 0, 3+len(options))
	parts = append(parts, "setx")
	parts = append(parts, options...)
	parts = append(parts, "path", `"`+translated+`"`)
	return strings.Join(parts, " "), true
}

func canPassThroughCmdInvocation(script string) bool {
	i := shellutil.SkipShellTokenSpaces(script, 0)
	exeToken, next := shellutil.NextShellToken(script, i)
	if shellutil.NormalizeShellExecutableName(shellutil.DropQuotes(exeToken)) != command.Cmd {
		return false
	}

	args := make([]string, 0, 4)
	for {
		token, end := shellutil.NextShellToken(script, next)
		if token == "" {
			break
		}
		args = append(args, token)
		next = end
	}
	if len(args) < 2 {
		return false
	}

	for idx, arg := range args {
		lower := strings.ToLower(shellutil.DropQuotes(arg))
		if lower == "/c" || lower == "/k" {
			for _, tail := range args[idx+1:] {
				if !isPowerShellSafeCmdArg(tail) {
					return false
				}
			}
			return idx+1 < len(args)
		}
		if !strings.HasPrefix(lower, "/") {
			return false
		}
	}

	return false
}

func isPowerShellSafeCmdArg(arg string) bool {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return false
	}
	if shellutil.DropQuotes(trimmed) != trimmed {
		if len(trimmed) < 2 {
			return false
		}
		quote := trimmed[0]
		inner := trimmed[1 : len(trimmed)-1]
		if quote == '\'' {
			return !strings.Contains(inner, "'") && !hasCmdVariableSyntax(inner)
		}
		return !strings.ContainsAny(inner, "`$\"") && !hasCmdVariableSyntax(inner)
	}
	return powerShellSafeUnquotedArgPattern.MatchString(trimmed) && !hasCmdVariableSyntax(trimmed)
}

func hasCmdVariableSyntax(text string) bool {
	return embeddedCmdVariablePattern.MatchString(text)
}

func stripMatchingQuotes(text string) string {
	if len(text) >= 2 && ((text[0] == '\'' && text[len(text)-1] == '\'') || (text[0] == '"' && text[len(text)-1] == '"')) {
		return text[1 : len(text)-1]
	}
	return text
}

func buildPowerShellWrapperRewriteEdit(
	file string,
	sm *sourcemap.SourceMap,
	item powerShellRun,
) (rules.TextEdit, bool) {
	return buildRunBodyRewriteEdit(file, sm, item.run, item.invocation.script)
}

func buildRunBodyRewriteEdit(
	file string,
	sm *sourcemap.SourceMap,
	run *instructions.RunCommand,
	newScript string,
) (rules.TextEdit, bool) {
	resolved, ok := dockerfile.ResolveRunSource(run, sm)
	if !ok {
		return rules.TextEdit{}, false
	}
	edit := sourceRangeEdit(
		file,
		resolved.Source,
		resolved.StartLine,
		resolved.ScriptIndex,
		resolved.ScriptIndex+len(resolved.Script),
		newScript,
	)
	if edit == nil {
		return rules.TextEdit{}, false
	}
	return *edit, true
}

func sourceRangeEdit(file, source string, startLine, byteStart, byteEnd int, newText string) *rules.TextEdit {
	if byteStart < 0 || byteEnd > len(source) || byteStart > byteEnd {
		return nil
	}

	startLineOffset, startCol := byteToLineCol(source, byteStart)
	endLineOffset, endCol := byteToLineCol(source, byteEnd)

	return &rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine+startLineOffset, startCol, startLine+endLineOffset, endCol),
		NewText:  newText,
	}
}

func byteToLineCol(s string, offset int) (int, int) {
	line := 0
	lineStart := 0
	for i := range offset {
		if s[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, offset - lineStart
}

func parseExplicitPowerShellInvocation(script string) (explicitPowerShellInvocation, bool) {
	i := shellutil.SkipShellTokenSpaces(script, 0)
	if i >= len(script) {
		return explicitPowerShellInvocation{}, false
	}

	if script[i] == '@' {
		i++
		i = shellutil.SkipShellTokenSpaces(script, i)
	}

	exeToken, next := shellutil.NextShellToken(script, i)
	if exeToken == "" {
		return explicitPowerShellInvocation{}, false
	}

	exe := shellutil.DropQuotes(exeToken)
	exeNorm := shellutil.NormalizeShellExecutableName(exe)
	if exeNorm != "powershell" && exeNorm != "pwsh" {
		return explicitPowerShellInvocation{}, false
	}

	var prefixArgs []string
	firstTokenAfterExe := true
	for {
		tokenStart := shellutil.SkipShellTokenSpaces(script, next)
		token, end := shellutil.NextShellToken(script, next)
		if token == "" {
			return explicitPowerShellInvocation{}, false
		}

		tokenNorm := strings.ToLower(shellutil.DropQuotes(token))
		if tokenNorm == "-command" || tokenNorm == "-c" {
			scriptStart := shellutil.SkipShellTokenSpaces(script, end)
			if scriptStart >= len(script) {
				return explicitPowerShellInvocation{}, false
			}
			return explicitPowerShellInvocation{
				executable:     exe,
				normalizedExec: exeNorm,
				prefixArgs:     prefixArgs,
				script:         normalizeCommandScript(script[scriptStart:]),
			}, true
		}

		if firstTokenAfterExe && !strings.HasPrefix(tokenNorm, "-") {
			parsedScript := normalizeCommandScript(script[tokenStart:])
			if shellutil.CanParsePowerShellScript(parsedScript) {
				return explicitPowerShellInvocation{
					executable:     exe,
					normalizedExec: exeNorm,
					script:         parsedScript,
				}, true
			}
			return explicitPowerShellInvocation{}, false
		}

		prefixArgs = append(prefixArgs, shellutil.DropQuotes(token))
		next = end
		firstTokenAfterExe = false
	}
}

func normalizeCommandScript(script string) string {
	trimmed := strings.TrimSpace(script)
	token, end := shellutil.NextShellToken(trimmed, 0)
	if token != "" && end == len(trimmed) {
		return shellutil.DropQuotes(token)
	}
	return trimmed
}

func leadingIndent(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferShellInstructionRule())
}
