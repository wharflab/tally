package powershell

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"

	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	shellutil "github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// PreferShellInstructionRuleCode is the full rule code for prefer-shell-instruction.
const PreferShellInstructionRuleCode = rules.TallyRulePrefix + "powershell/prefer-shell-instruction"

const powerShellPrelude = "$ErrorActionPreference = 'Stop'; " +
	"$PSNativeCommandUseErrorActionPreference = $true; " +
	"$ProgressPreference = 'SilentlyContinue';"

var powerShellSafeUnquotedArgPattern = regexp.MustCompile(`^[A-Za-z0-9_./:\\=-]+$`)

var cmdPathVariablePattern = regexp.MustCompile(`(?i)%path%|!path!`)

var embeddedCmdVariablePattern = regexp.MustCompile(`(?i)(%[a-z_][a-z0-9_]*(?::[^%]+)?%|![a-z_][a-z0-9_]*!|%%~?[a-z]|%~?[0-9])`)

var unsafeCmdBuiltinsForPowerShell = map[string]bool{
	"assoc":      true,
	"break":      true,
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
	"mklink":     true,
	"move":       true,
	"path":       true,
	"pause":      true,
	"prompt":     true,
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
	usesCommandArg bool
}

type powerShellRun struct {
	run        *instructions.RunCommand
	invocation explicitPowerShellInvocation
}

type powerShellCluster struct {
	startIdx       int
	shellBeforeCmd []string
	shellBefore    shellutil.Variant
	runs           []powerShellRun
}

type runtimeShellDependantTarget struct {
	cmdline instructions.ShellDependantCmdLine
	loc     []parser.Range
}

// Check runs the rule.
func (r *PreferShellInstructionRule) Check(input rules.LintInput) []rules.Violation {
	if len(powershellStages(input)) == 0 {
		return nil
	}

	meta := r.Metadata()
	sm := input.SourceMap()

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		violations = append(violations, r.checkStage(input, meta, sm, stageIdx, stage)...)
	}

	return violations
}

func (r *PreferShellInstructionRule) checkStage(
	input rules.LintInput,
	meta rules.RuleMetadata,
	sm *sourcemap.SourceMap,
	stageIdx int,
	stage instructions.Stage,
) []rules.Violation {
	currentShell := initialStageShellVariant(input.Semantic, stageIdx)
	currentShellCmd := initialStageShellCmd(input.Semantic, stageIdx)
	escapeToken := rune(0)
	if input.AST != nil {
		escapeToken = input.AST.EscapeToken
	}

	violations := make([]rules.Violation, 0, 1)
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
		if v := buildPreferShellViolation(meta, input.File, sm, stageIdx, stage.Commands, cluster, escapeToken); v != nil {
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
			currentShellCmd = append([]string(nil), c.Shell...)
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

			invocation, ok := parsePowerShellWrapperClusterCandidate(c.CmdLine[0], currentShell)
			if !ok {
				if cluster.startIdx >= 0 && canKeepCmdRunUnderPowerShell(c.CmdLine[0], currentShell) {
					continue
				}
				flush()
				continue
			}

			if cluster.startIdx >= 0 && !samePowerShellWrapperShape(cluster.runs[0].invocation, invocation) {
				flush()
			}
			if cluster.startIdx < 0 {
				cluster.startIdx = cmdIdx
				cluster.shellBefore = currentShell
				cluster.shellBeforeCmd = append([]string(nil), currentShellCmd...)
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

func initialStageShellCmd(sem *semantic.Model, stageIdx int) []string {
	if sem == nil {
		return append([]string(nil), semantic.DefaultShell...)
	}
	info := sem.StageInfo(stageIdx)
	if info == nil {
		return append([]string(nil), semantic.DefaultShell...)
	}
	if len(info.ShellSetting.Shell) > 0 {
		return append([]string(nil), info.ShellSetting.Shell...)
	}
	if info.IsWindows() {
		return semantic.DefaultWindowsShell()
	}
	return append([]string(nil), semantic.DefaultShell...)
}

func buildPreferShellViolation(
	meta rules.RuleMetadata,
	file string,
	sm *sourcemap.SourceMap,
	stageIdx int,
	stageCommands []instructions.Command,
	cluster powerShellCluster,
	escapeToken rune,
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

	if fix := buildSuggestedFix(file, sm, stageCommands, cluster, escapeToken); fix != nil {
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
	escapeToken rune,
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

	shellArray := formatShellArray(shellArgs)
	if shellArray == "" {
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

	runEdits, ok := collectStageRunEditsForPowerShell(file, sm, stageCommands, cluster, escapeToken)
	if !ok {
		return nil
	}

	edits := make([]rules.TextEdit, 0, len(runEdits)+1)
	edits = append(edits, rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine, 0, startLine, 0),
		NewText:  indent + "SHELL " + shellArray + "\n",
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
		if !samePowerShellWrapperShape(first, item.invocation) {
			return false
		}
	}

	return true
}

func samePowerShellWrapperShape(a, b explicitPowerShellInvocation) bool {
	if !strings.EqualFold(a.normalizedExec, b.normalizedExec) {
		return false
	}
	return slices.EqualFunc(a.prefixArgs, b.prefixArgs, strings.EqualFold)
}

func collectStageRunEditsForPowerShell(
	file string,
	sm *sourcemap.SourceMap,
	stageCommands []instructions.Command,
	cluster powerShellCluster,
	escapeToken rune,
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
				nextEdits, stop, ok := collectRuntimeShellDependantEdits(
					file,
					sm,
					edits,
					cluster,
					escapeToken,
					runtimeShellDependantTarget{
						cmdline: cmd.ShellDependantCmdLine,
						loc:     cmd.Location(),
					},
				)
				if !ok {
					return nil, false
				}
				edits = nextEdits
				if stop {
					return edits, true
				}
			}
		case *instructions.EntrypointCommand:
			if cmd.PrependShell {
				nextEdits, stop, ok := collectRuntimeShellDependantEdits(
					file,
					sm,
					edits,
					cluster,
					escapeToken,
					runtimeShellDependantTarget{
						cmdline: cmd.ShellDependantCmdLine,
						loc:     cmd.Location(),
					},
				)
				if !ok {
					return nil, false
				}
				edits = nextEdits
				if stop {
					return edits, true
				}
			}
		case *instructions.HealthCheckCommand:
			if healthcheckUsesShell(cmd) {
				return appendRestoreShellEdit(file, sm, edits, cluster.shellBeforeCmd, cmd.Location())
			}
		case *instructions.RunCommand:
			if !cmd.PrependShell {
				continue
			}

			if item, ok := clusterRuns[cmd]; ok {
				nextEdits, ok := appendClusterRunRewriteEdit(file, sm, edits, item, cluster.shellBefore, escapeToken)
				if !ok {
					return nil, false
				}
				edits = nextEdits
				continue
			}

			nextEdits, ok := collectImpactedRunEditsForPowerShell(file, sm, edits, cluster, escapeToken, cmd)
			if !ok {
				return nil, false
			}
			edits = nextEdits
		}
	}

	return edits, true
}

func collectRuntimeShellDependantEdits(
	file string,
	sm *sourcemap.SourceMap,
	edits []rules.TextEdit,
	cluster powerShellCluster,
	escapeToken rune,
	target runtimeShellDependantTarget,
) ([]rules.TextEdit, bool, bool) {
	edit, needed, ok := adaptRuntimeCommandForPowerShell(
		file,
		sm,
		cluster.shellBefore,
		escapeToken,
		target.cmdline,
		target.loc,
	)
	if !ok {
		nextEdits, ok := appendRestoreShellEdit(file, sm, edits, cluster.shellBeforeCmd, target.loc)
		return nextEdits, true, ok
	}
	if !needed {
		return edits, false, true
	}
	return append(edits, edit), false, true
}

func appendClusterRunRewriteEdit(
	file string,
	sm *sourcemap.SourceMap,
	edits []rules.TextEdit,
	item powerShellRun,
	shellBefore shellutil.Variant,
	escapeToken rune,
) ([]rules.TextEdit, bool) {
	edit, ok := buildPowerShellWrapperRewriteEdit(file, sm, item, shellBefore, escapeToken)
	if !ok {
		return nil, false
	}
	return append(edits, edit), true
}

func collectImpactedRunEditsForPowerShell(
	file string,
	sm *sourcemap.SourceMap,
	edits []rules.TextEdit,
	cluster powerShellCluster,
	escapeToken rune,
	run *instructions.RunCommand,
) ([]rules.TextEdit, bool) {
	edit, needed, ok := adaptImpactedRunForPowerShell(file, sm, cluster.shellBefore, escapeToken, run)
	if !ok {
		return appendRestoreShellEdit(file, sm, edits, cluster.shellBeforeCmd, run.Location())
	}
	if !needed {
		return edits, true
	}
	return append(edits, edit), true
}

func appendRestoreShellEdit(
	file string,
	sm *sourcemap.SourceMap,
	edits []rules.TextEdit,
	shellCmd []string,
	loc []parser.Range,
) ([]rules.TextEdit, bool) {
	edit, ok := buildShellInsertionEdit(file, sm, shellCmd, loc)
	if !ok {
		return nil, false
	}
	return append(edits, edit), true
}

func adaptRuntimeCommandForPowerShell(
	file string,
	sm *sourcemap.SourceMap,
	shellBefore shellutil.Variant,
	escapeToken rune,
	cmdline instructions.ShellDependantCmdLine,
	loc []parser.Range,
) (rules.TextEdit, bool, bool) {
	if !cmdline.PrependShell || len(cmdline.CmdLine) == 0 {
		return rules.TextEdit{}, false, false
	}

	invocation, ok := parseExplicitPowerShellInvocation(cmdline.CmdLine[0])
	if !ok {
		return rules.TextEdit{}, false, false
	}

	newScript, ok := normalizePowerShellWrapperScriptForInsertedShell(
		cmdline.CmdLine[0],
		invocation,
		shellBefore,
		escapeToken,
		leadingIndentForLocation(sm, loc),
	)
	if !ok || strings.TrimSpace(newScript) == "" {
		return rules.TextEdit{}, false, false
	}

	edit, ok := buildShellDependantCommandRewriteEdit(file, sm, loc, cmdline.CmdLine[0], newScript)
	if !ok {
		return rules.TextEdit{}, false, false
	}

	return edit, true, true
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
	escapeToken rune,
	run *instructions.RunCommand,
) (rules.TextEdit, bool, bool) {
	if len(run.CmdLine) == 0 {
		return rules.TextEdit{}, false, false
	}
	if invocation, ok := parseExplicitPowerShellInvocation(run.CmdLine[0]); ok {
		newScript, ok := normalizePowerShellWrapperScriptForInsertedShell(
			run.CmdLine[0],
			invocation,
			shellBefore,
			escapeToken,
			leadingIndentForLocation(sm, run.Location()),
		)
		if !ok || strings.TrimSpace(newScript) == "" {
			return rules.TextEdit{}, false, false
		}

		edit, ok := buildRunBodyRewriteEdit(file, sm, run, newScript)
		if !ok {
			return rules.TextEdit{}, false, false
		}
		return edit, true, true
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
	if rewritten, ok := rewriteSetEnvironmentCommandForPowerShell(cmd); ok {
		return rewritten, true, true
	}

	if unsafeCmdBuiltinsForPowerShell[cmd.Name] {
		return "", false, false
	}

	if rewritten, ok := rewriteSetXPathCommandForPowerShell(cmd); ok {
		return rewritten, true, true
	}

	if !analysis.HasVariableReferences {
		return "", false, true
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

func normalizePowerShellWrapperScriptForInsertedShell(
	script string,
	invocation explicitPowerShellInvocation,
	shellBefore shellutil.Variant,
	escapeToken rune,
	lineIndent string,
) (string, bool) {
	if shellBefore != shellutil.VariantCmd {
		return invocation.script, true
	}

	parts := shellutil.ExtractChainedCommands(script, shellutil.VariantCmd)
	if len(parts) == 0 {
		return "", false
	}

	firstInvocation, ok := parseExplicitPowerShellInvocation(parts[0])
	if !ok {
		return "", false
	}

	statements := []string{strings.TrimSpace(firstInvocation.script)}
	for _, part := range parts[1:] {
		rewritten, changed, ok := rewriteCompatibleCmdScriptForPowerShell(part)
		if !ok {
			return "", false
		}
		if changed {
			statements = append(statements, strings.TrimSpace(rewritten))
			continue
		}
		statements = append(statements, strings.TrimSpace(part))
	}

	return formatPowerShellDockerfileStatements(statements, escapeToken, lineIndent), true
}

func parsePowerShellWrapperClusterCandidate(
	script string,
	shellBefore shellutil.Variant,
) (explicitPowerShellInvocation, bool) {
	if invocation, ok := parseExplicitPowerShellInvocation(script); ok {
		return invocation, true
	}
	if shellBefore != shellutil.VariantCmd {
		return explicitPowerShellInvocation{}, false
	}

	parts := shellutil.ExtractChainedCommands(script, shellutil.VariantCmd)
	if len(parts) < 2 {
		return explicitPowerShellInvocation{}, false
	}

	invocation, ok := parseExplicitPowerShellInvocation(parts[0])
	if !ok {
		return explicitPowerShellInvocation{}, false
	}
	for _, part := range parts[1:] {
		if _, _, ok := rewriteCompatibleCmdScriptForPowerShell(part); !ok {
			return explicitPowerShellInvocation{}, false
		}
	}

	return invocation, true
}

func formatPowerShellDockerfileStatements(statements []string, escapeToken rune, lineIndent string) string {
	filtered := make([]string, 0, len(statements))
	for _, stmt := range statements {
		trimmed := strings.TrimSpace(stmt)
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	if escapeToken == 0 {
		escapeToken = '\\'
	}

	var b strings.Builder
	for i, stmt := range filtered {
		b.WriteString(stmt)
		if i == len(filtered)-1 {
			continue
		}
		b.WriteString("; ")
		b.WriteRune(escapeToken)
		b.WriteString("\n")
		b.WriteString(lineIndent)
		b.WriteString("    ")
	}

	return b.String()
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

func rewriteSetEnvironmentCommandForPowerShell(cmd shellutil.CommandInfo) (string, bool) {
	if cmd.Name != "set" || len(cmd.Args) != 1 {
		return "", false
	}

	assignment := stripMatchingQuotes(cmd.Args[0])
	name, value, ok := strings.Cut(assignment, "=")
	if !ok || name == "" || !isSimplePowerShellEnvVarName(name) {
		return "", false
	}

	translated, ok := translateCmdEnvironmentValueForPowerShell(value)
	if !ok {
		return "", false
	}

	return "$env:" + normalizePowerShellEnvVarName(name) + " = " + translated, true
}

func isSimplePowerShellEnvVarName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func normalizePowerShellEnvVarName(name string) string {
	if strings.EqualFold(name, "path") {
		return "Path"
	}
	return name
}

func translateCmdEnvironmentValueForPowerShell(value string) (string, bool) {
	if strings.Contains(value, "`") {
		return "", false
	}

	var b strings.Builder
	for i := 0; i < len(value); {
		switch value[i] {
		case '%', '!':
			end := strings.IndexByte(value[i+1:], value[i])
			if end < 0 {
				return "", false
			}
			end += i + 1

			name := value[i+1 : end]
			if name == "" || strings.Contains(name, ":") || !isSimplePowerShellEnvVarName(name) {
				return "", false
			}
			b.WriteString("$env:")
			b.WriteString(normalizePowerShellEnvVarName(name))
			i = end + 1
		default:
			switch value[i] {
			case '"':
				b.WriteString("`\"")
			case '$':
				b.WriteString("`$")
			default:
				b.WriteByte(value[i])
			}
			i++
		}
	}

	return `"` + b.String() + `"`, true
}

func canKeepCmdRunUnderPowerShell(script string, shellBefore shellutil.Variant) bool {
	if shellBefore != shellutil.VariantCmd {
		return false
	}
	_, _, ok := rewriteCompatibleCmdScriptForPowerShell(script)
	return ok
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
	shellBefore shellutil.Variant,
	escapeToken rune,
) (rules.TextEdit, bool) {
	newScript, ok := normalizePowerShellWrapperScriptForInsertedShell(
		item.run.CmdLine[0],
		item.invocation,
		shellBefore,
		escapeToken,
		leadingIndentForLocation(sm, item.run.Location()),
	)
	if !ok || strings.TrimSpace(newScript) == "" {
		return rules.TextEdit{}, false
	}
	return buildRunBodyRewriteEdit(file, sm, item.run, newScript)
}

func buildRunBodyRewriteEdit(
	file string,
	sm *sourcemap.SourceMap,
	run *instructions.RunCommand,
	newScript string,
) (rules.TextEdit, bool) {
	resolved, ok := dockerfile.ResolveRunSource(run, sm)
	if ok {
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

	source, startLine, scriptIndex, ok := resolveRunInstructionScriptRange(run, sm)
	if !ok {
		return rules.TextEdit{}, false
	}

	edit := sourceRangeEdit(file, source, startLine, scriptIndex, len(source), newScript)
	if edit == nil {
		return rules.TextEdit{}, false
	}
	return *edit, true
}

func buildShellDependantCommandRewriteEdit(
	file string,
	sm *sourcemap.SourceMap,
	loc []parser.Range,
	script string,
	newScript string,
) (rules.TextEdit, bool) {
	source, startLine, scriptIndex, ok := resolveShellDependantInstructionScriptRange(loc, sm, script)
	if !ok {
		return rules.TextEdit{}, false
	}

	edit := sourceRangeEdit(file, source, startLine, scriptIndex, scriptIndex+len(script), newScript)
	if edit == nil {
		return rules.TextEdit{}, false
	}

	return *edit, true
}

func resolveRunInstructionScriptRange(
	run *instructions.RunCommand,
	sm *sourcemap.SourceMap,
) (string, int, int, bool) {
	if run == nil || sm == nil {
		return "", 0, 0, false
	}

	source, startLine := dockerfile.RunSourceScript(run, sm)
	if source == "" || startLine == 0 {
		return "", 0, 0, false
	}

	scriptIndex := strings.IndexFunc(source, func(r rune) bool {
		return r != ' ' && r != '\t' && r != '\r' && r != '\n'
	})
	if scriptIndex < 0 {
		return "", 0, 0, false
	}

	return source, startLine, scriptIndex, true
}

func resolveShellDependantInstructionScriptRange(
	loc []parser.Range,
	sm *sourcemap.SourceMap,
	script string,
) (string, int, int, bool) {
	if len(loc) == 0 || sm == nil || script == "" {
		return "", 0, 0, false
	}

	startLine := loc[0].Start.Line
	endLine := loc[len(loc)-1].End.Line

	var lines []string
	for lineIdx := startLine - 1; lineIdx < endLine; lineIdx++ {
		if lineIdx >= 0 && lineIdx < sm.LineCount() {
			lines = append(lines, sm.Line(lineIdx))
		}
	}
	if len(lines) == 0 {
		return "", 0, 0, false
	}

	source := strings.Join(lines, "\n")
	scriptIndex := strings.Index(source, script)
	if scriptIndex < 0 {
		return "", 0, 0, false
	}

	return source, startLine, scriptIndex, true
}

func buildShellInsertionEdit(
	file string,
	sm *sourcemap.SourceMap,
	shellCmd []string,
	loc []parser.Range,
) (rules.TextEdit, bool) {
	if len(shellCmd) == 0 || len(loc) == 0 {
		return rules.TextEdit{}, false
	}

	shellArray := formatShellArray(shellCmd)
	if shellArray == "" {
		return rules.TextEdit{}, false
	}

	startLine := loc[0].Start.Line
	indent := ""
	if sm != nil && startLine > 0 {
		startLine = sm.EffectiveStartLine(startLine, nil)
		indent = leadingIndent(sm.Line(startLine - 1))
	}

	return rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine, 0, startLine, 0),
		NewText:  indent + "SHELL " + shellArray + "\n",
	}, true
}

func sourceRangeEdit(file, source string, startLine, byteStart, byteEnd int, newText string) *rules.TextEdit {
	if byteStart < 0 || byteEnd > len(source) || byteStart > byteEnd {
		return nil
	}

	startLineOffset, startCol := sourcemap.ByteToLineCol(source, byteStart)
	endLineOffset, endCol := sourcemap.ByteToLineCol(source, byteEnd)

	return &rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine+startLineOffset, startCol, startLine+endLineOffset, endCol),
		NewText:  newText,
	}
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
				usesCommandArg: true,
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

func leadingIndentForLocation(sm *sourcemap.SourceMap, loc []parser.Range) string {
	if sm == nil || len(loc) == 0 || loc[0].Start.Line <= 0 {
		return ""
	}

	lineNum := loc[0].Start.Line - 1
	if lineNum < 0 || lineNum >= sm.LineCount() {
		return ""
	}

	return leadingIndent(sm.Line(lineNum))
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewPreferShellInstructionRule())
}
