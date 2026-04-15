package powershell

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/semantic"
	shellutil "github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// ErrorActionPreferenceRuleCode is the full rule code.
const ErrorActionPreferenceRuleCode = rules.TallyRulePrefix + "powershell/error-action-preference"

const defaultPwshExecutable = "pwsh"

// ErrorActionPreferenceConfig is the configuration for the rule.
type ErrorActionPreferenceConfig struct {
	// MinStatements is the minimum number of PowerShell statements to trigger the rule.
	// Default is 2 (multi-statement RUNs). Set to 1 to also catch non-terminating
	// errors on single-command RUNs.
	MinStatements *int `json:"min-statements,omitempty" koanf:"min-statements"`
}

// DefaultErrorActionPreferenceConfig returns the default configuration.
func DefaultErrorActionPreferenceConfig() ErrorActionPreferenceConfig {
	minStatements := 2
	return ErrorActionPreferenceConfig{
		MinStatements: &minStatements,
	}
}

// ErrorActionPreferenceRule warns when multi-statement PowerShell RUN instructions
// lack $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true.
type ErrorActionPreferenceRule struct{}

// NewErrorActionPreferenceRule creates a new rule instance.
func NewErrorActionPreferenceRule() *ErrorActionPreferenceRule {
	return &ErrorActionPreferenceRule{}
}

// Metadata returns the rule metadata.
func (r *ErrorActionPreferenceRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code: ErrorActionPreferenceRuleCode,
		Name: "Require PowerShell error-handling preferences",
		Description: "PowerShell RUN should set $ErrorActionPreference = 'Stop'" +
			" and $PSNativeCommandUseErrorActionPreference = $true",
		DocURL:          rules.TallyDocURL(ErrorActionPreferenceRuleCode),
		DefaultSeverity: rules.SeverityWarning,
		Category:        "correctness",
		FixPriority:     96, //nolint:mnd // After prefer-shell-instruction (95), before heredoc (100).
	}
}

// DefaultConfig returns the default configuration.
func (r *ErrorActionPreferenceRule) DefaultConfig() any {
	return DefaultErrorActionPreferenceConfig()
}

// Check runs the rule.
func (r *ErrorActionPreferenceRule) Check(input rules.LintInput) []rules.Violation {
	if len(powershellStages(input)) == 0 {
		return nil
	}

	cfg := r.resolveConfig(input.Config)
	minStatements := 2
	if cfg.MinStatements != nil && *cfg.MinStatements >= 1 {
		minStatements = *cfg.MinStatements
	}

	meta := r.Metadata()
	sm := input.SourceMap()

	var violations []rules.Violation
	for stageIdx, stage := range input.Stages {
		stageInfo := stageInfoForIndex(input.Semantic, stageIdx)
		p := checkParams{
			file:          input.File,
			meta:          meta,
			sm:            sm,
			stageIdx:      stageIdx,
			info:          stageInfo,
			minStatements: minStatements,
		}
		violations = append(violations, r.checkStage(input, sm, stageIdx, stage, stageInfo, p)...)
	}

	return violations
}

func stageInfoForIndex(sem *semantic.Model, idx int) *semantic.StageInfo {
	if sem == nil {
		return nil
	}
	return sem.StageInfo(idx)
}

// checkParams groups shared parameters for per-RUN checking.
type checkParams struct {
	file          string
	meta          rules.RuleMetadata
	sm            *sourcemap.SourceMap
	stageIdx      int
	info          *semantic.StageInfo
	minStatements int
}

func (r *ErrorActionPreferenceRule) checkStage(
	input rules.LintInput,
	_ *sourcemap.SourceMap,
	stageIdx int,
	stage instructions.Stage,
	_ *semantic.StageInfo,
	params checkParams,
) []rules.Violation {
	var violations []rules.Violation

	// Track whether a stage-level SHELL fix has already been emitted so that
	// multiple RUN violations in the same stage don't produce duplicate edits
	// on the same SHELL instruction.
	stageFixEmitted := false

	// Track the effective SHELL args through the stage to detect prelude.
	activeShellCmd := initialStageShellCmd(input.Semantic, stageIdx)

	for _, cmd := range stage.Commands {
		if sc, ok := cmd.(*instructions.ShellCommand); ok && len(sc.Shell) > 0 {
			activeShellCmd = sc.Shell
			stageFixEmitted = false // new SHELL resets tracking
			continue
		}

		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell || len(run.CmdLine) == 0 {
			continue
		}

		v := r.checkRun(params, run, run.CmdLine[0], activeShellCmd)
		if v != nil {
			// Suppress duplicate stage-level SHELL fixes. When multiple
			// RUNs in the same SHELL-based stage trigger, only the first
			// violation carries the SHELL edit; the rest report without a
			// fix to avoid overlapping edits on the same SHELL line.
			// Wrapper fixes (explicit powershell -Command) are per-RUN
			// body insertions and are never suppressed.
			variant := shellutil.VariantFromShellCmd(activeShellCmd)
			if v.SuggestedFix != nil && variant.IsPowerShell() && stageFixEmitted {
				v.SuggestedFix = nil
			} else if v.SuggestedFix != nil && variant.IsPowerShell() {
				stageFixEmitted = true
			}
			violations = append(violations, *v)
		}
	}

	return violations
}

func (r *ErrorActionPreferenceRule) checkRun(
	p checkParams,
	run *instructions.RunCommand,
	script string,
	activeShellCmd []string,
) *rules.Violation {
	// Determine effective shell variant at this RUN.
	variant := shellutil.VariantBash
	if p.info != nil && len(run.Location()) > 0 {
		variant = p.info.ShellVariantAtLine(run.Location()[0].Start.Line)
	}

	if variant.IsPowerShell() {
		return r.checkPowerShellRun(p, run, script, activeShellCmd)
	}

	// Path B: explicit powershell/pwsh wrapper in a non-PowerShell shell.
	return r.checkExplicitWrapper(p, run, script)
}

func (r *ErrorActionPreferenceRule) checkPowerShellRun(
	p checkParams,
	run *instructions.RunCommand,
	script string,
	activeShellCmd []string,
) *rules.Violation {
	count := shellutil.CountChainedCommands(script, shellutil.VariantPowerShell)
	if count < p.minStatements {
		return nil
	}

	// Check SHELL-level prelude using the tracked active shell args.
	shellHasStop, shellHasNative := shellPreludeStateFromCmd(activeShellCmd)

	// Check script-body prelude.
	scriptHasStop, scriptHasNative, scriptHasWrongStop := scanScriptPrelude(script)

	hasStop := shellHasStop || scriptHasStop
	hasNative := shellHasNative || scriptHasNative

	if hasStop && hasNative {
		return nil
	}

	loc := rules.NewLocationFromRanges(p.file, run.Location())
	if loc.IsFileLevel() {
		return nil
	}

	msg, detail := buildViolationMessage(hasStop, hasNative, scriptHasWrongStop)
	v := rules.NewViolation(loc, p.meta.Code, msg, p.meta.DefaultSeverity).
		WithDocURL(p.meta.DocURL).
		WithDetail(detail)
	v.StageIndex = p.stageIdx

	if fix := r.buildStageFix(p, run, activeShellCmd, !hasStop, !hasNative); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

func (r *ErrorActionPreferenceRule) checkExplicitWrapper(
	p checkParams,
	run *instructions.RunCommand,
	script string,
) *rules.Violation {
	invocation, ok := parseExplicitPowerShellInvocation(script)
	if !ok {
		return nil
	}

	innerScript := invocation.script
	count := shellutil.CountChainedCommands(innerScript, shellutil.VariantPowerShell)
	if count < p.minStatements {
		return nil
	}

	scriptHasStop, scriptHasNative, scriptHasWrongStop := scanScriptPrelude(innerScript)

	if scriptHasStop && scriptHasNative {
		return nil
	}

	loc := rules.NewLocationFromRanges(p.file, run.Location())
	if loc.IsFileLevel() {
		return nil
	}

	msg, detail := buildViolationMessage(scriptHasStop, scriptHasNative, scriptHasWrongStop)
	v := rules.NewViolation(loc, p.meta.Code, msg, p.meta.DefaultSeverity).
		WithDocURL(p.meta.DocURL).
		WithDetail(detail)
	v.StageIndex = p.stageIdx

	if fix := r.buildWrapperFix(p, run, script, invocation, !scriptHasStop, !scriptHasNative); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

// buildWrapperFix inserts missing prelude(s) at the inner script start of an
// explicit wrapper. Uses a zero-width insertion so it never conflicts with
// edits from other rules targeting the same RUN body.
//
// Because BuildKit flattens continuation lines in CmdLine[0], we cannot use
// ResolveRunSource (which tries to find the flattened script in raw source).
// Instead we scan the raw source lines for the first inner-script token.
func (r *ErrorActionPreferenceRule) buildWrapperFix(
	p checkParams,
	run *instructions.RunCommand,
	_ string,
	invocation explicitPowerShellInvocation,
	needStop bool,
	needNative bool,
) *rules.SuggestedFix {
	if p.sm == nil || len(run.Location()) == 0 {
		return nil
	}

	startLine := run.Location()[0].Start.Line
	endLine := run.Location()[len(run.Location())-1].End.Line

	// Use Snippet to get the raw source (0-based line args).
	source := p.sm.Snippet(startLine-1, endLine-1)
	if source == "" {
		return nil
	}

	// Anchor the search to text after the -Command argument so we don't
	// accidentally match a token that appears in the outer wrapper prefix
	// (e.g., `RUN pwsh -Command "pwsh -Command ..."`).
	commandArgIdx := findCommandArgEnd(source)

	firstToken := firstNonWhitespaceWord(invocation.script)
	if firstToken == "" {
		return nil
	}
	relIdx := strings.Index(source[commandArgIdx:], firstToken)
	if relIdx < 0 {
		return nil
	}
	insertByte := commandArgIdx + relIdx

	prelude := buildPreludeString(needStop, needNative) + " "
	insertLine, insertCol := sourcemap.ByteToLineCol(source, insertByte)

	return &rules.SuggestedFix{
		Description: "Add PowerShell error-handling preferences to wrapper script",
		Safety:      rules.FixSuggestion,
		Priority:    p.meta.FixPriority,
		Edits: []rules.TextEdit{
			{
				// Zero-width insertion: Start == End.
				Location: rules.NewRangeLocation(
					p.file,
					startLine+insertLine, insertCol,
					startLine+insertLine, insertCol,
				),
				NewText: prelude,
			},
		},
	}
}

// firstNonWhitespaceWord returns the first whitespace-delimited word from s.
func firstNonWhitespaceWord(s string) string {
	trimmed := strings.TrimSpace(s)
	if idx := strings.IndexAny(trimmed, " \t\n\r;"); idx > 0 {
		return trimmed[:idx]
	}
	return trimmed
}

// findCommandArgEnd returns the byte offset in source just past the
// "-Command" (or "-c") argument of a `powershell`/`pwsh` wrapper.
// If not found, returns 0 so the caller falls back to a full search.
func findCommandArgEnd(source string) int {
	lower := strings.ToLower(source)
	for _, flag := range []string{"-command", "-c"} {
		idx := strings.Index(lower, flag)
		if idx >= 0 {
			return idx + len(flag)
		}
	}
	return 0
}

// shellPreludeStateFromCmd checks the SHELL command args for the two prelude variables.
func shellPreludeStateFromCmd(shellCmd []string) (hasStop, hasNative bool) {
	if len(shellCmd) <= 1 {
		return false, false
	}

	// The prelude lives in the last SHELL arg (e.g., the string after
	// "-Command"). Parse it with the same assignment-based logic used for
	// script bodies so we don't misclassify unrelated tokens like
	// "Stop-Process" or a stray "$true".
	script := shellCmd[len(shellCmd)-1]
	hasStop, hasNative, _ = scanScriptPrelude(script)
	return hasStop, hasNative
}

// scanScriptPrelude checks the leading PowerShell assignments in a script body.
// It stops at the first non-assignment statement so that preferences set after
// commands (e.g., `Invoke-WebRequest ...; $ErrorActionPreference = 'Stop'`) are
// not treated as a prelude -- the risky command already ran before fail-fast was
// enabled.
func scanScriptPrelude(script string) (hasStop, hasNative, hasWrongStop bool) {
	stmts := shellutil.ExtractChainedCommands(script, shellutil.VariantPowerShell)
	for _, stmt := range stmts {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		name, value, ok := shellutil.PowerShellAssignment(trimmed)
		if !ok {
			break // first non-assignment ends the prelude
		}

		switch {
		case strings.EqualFold(name, "$ErrorActionPreference"):
			if strings.EqualFold(shellutil.DropQuotes(value), "Stop") {
				hasStop = true
			} else {
				hasWrongStop = true
			}
		case strings.EqualFold(name, "$PSNativeCommandUseErrorActionPreference"):
			if strings.EqualFold(strings.TrimSpace(value), "$true") {
				hasNative = true
			}
		}
	}
	return hasStop, hasNative, hasWrongStop
}

func buildViolationMessage(hasStop, hasNative, hasWrongStop bool) (msg, detail string) {
	switch {
	case !hasStop && !hasNative:
		if hasWrongStop {
			msg = "PowerShell RUN sets $ErrorActionPreference to a non-Stop value and is missing $PSNativeCommandUseErrorActionPreference"
			detail = "$ErrorActionPreference should be 'Stop' so that errors are not silently swallowed. " +
				"$PSNativeCommandUseErrorActionPreference = $true extends this to native command exit codes (PowerShell 7.3+)."
		} else {
			msg = "PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true"
			detail = "Without $ErrorActionPreference = 'Stop', PowerShell silently continues after non-terminating errors. " +
				"$PSNativeCommandUseErrorActionPreference = $true extends error handling to native command exit codes (PowerShell 7.3+)."
		}
	case !hasStop:
		if hasWrongStop {
			msg = "PowerShell RUN sets $ErrorActionPreference to a non-Stop value"
			detail = "$ErrorActionPreference should be 'Stop' so that non-terminating errors halt the build."
		} else {
			msg = "PowerShell RUN is missing $ErrorActionPreference = 'Stop'"
			detail = "Without $ErrorActionPreference = 'Stop', PowerShell silently continues after non-terminating errors."
		}
	case !hasNative:
		msg = "PowerShell RUN is missing $PSNativeCommandUseErrorActionPreference = $true"
		detail = "Without $PSNativeCommandUseErrorActionPreference = $true, non-zero exit codes from native commands (git, dotnet, curl) " +
			"are ignored even when $ErrorActionPreference is 'Stop' (PowerShell 7.3+)."
	}
	return msg, detail
}

// buildStageFix creates a fix that modifies or inserts a SHELL instruction at stage level.
func (r *ErrorActionPreferenceRule) buildStageFix(
	p checkParams,
	_ *instructions.RunCommand,
	activeShellCmd []string,
	needStop bool,
	needNative bool,
) *rules.SuggestedFix {
	if p.info == nil || p.info.Stage == nil {
		return nil
	}

	shellVariant := shellutil.VariantFromShellCmd(activeShellCmd)
	if !shellVariant.IsPowerShell() {
		return nil
	}

	priority := p.meta.FixPriority

	// Find the actual SHELL instruction in the stage commands to modify.
	if shellCmd := findPowerShellInstruction(p.info.Stage.Commands); shellCmd != nil {
		return r.buildShellModifyFixFromCmd(p.file, p.sm, shellCmd, priority, needStop, needNative)
	}

	// No SHELL instruction found: insert one after FROM.
	return r.buildShellInsertFix(p.file, p.sm, p.info, priority, needStop, needNative)
}

func findPowerShellInstruction(commands []instructions.Command) *instructions.ShellCommand {
	for _, cmd := range commands {
		sc, ok := cmd.(*instructions.ShellCommand)
		if !ok || len(sc.Shell) == 0 {
			continue
		}
		if shellutil.VariantFromShellCmd(sc.Shell).IsPowerShell() {
			return sc
		}
	}
	return nil
}

func (r *ErrorActionPreferenceRule) buildShellModifyFixFromCmd(
	file string,
	sm *sourcemap.SourceMap,
	shellCmd *instructions.ShellCommand,
	priority int,
	needStop bool,
	needNative bool,
) *rules.SuggestedFix {
	if sm == nil || len(shellCmd.Location()) == 0 {
		return nil
	}
	shellLine := shellCmd.Location()[0].Start.Line
	if shellLine <= 0 {
		return nil
	}

	sourceLine := sm.Line(shellLine - 1)
	if sourceLine == "" {
		return nil
	}

	preludeToAdd := buildPreludeString(needStop, needNative)

	// Determine where and how to insert the prelude into the SHELL instruction.
	// Format: SHELL ["exe", "args...", "last-arg"]
	existing := extractShellLastArg(sourceLine)
	isCommandOnly := strings.EqualFold(strings.TrimSpace(existing), "-Command")

	if isCommandOnly {
		// Last arg is just "-Command" — add a new array element with the prelude.
		// Insert `, "<prelude>"` before the closing `]`.
		closeBracket := strings.LastIndex(sourceLine, "]")
		if closeBracket < 0 {
			return nil
		}
		newText := `, "` + preludeToAdd + `"`
		return &rules.SuggestedFix{
			Description: fmt.Sprintf("Add %s to SHELL instruction", preludeToAdd),
			Safety:      rules.FixSuggestion,
			Priority:    priority,
			Edits: []rules.TextEdit{
				{
					Location: rules.NewRangeLocation(
						file, shellLine, closeBracket, shellLine, closeBracket,
					),
					NewText: newText,
				},
			},
		}
	}

	// Last arg already has content (e.g., existing prelude). Append the missing
	// prelude(s) just before the closing quote of that arg.
	lastQuote := strings.LastIndex(sourceLine, `"`)
	if lastQuote < 0 {
		return nil
	}
	sep := " "
	if !strings.HasSuffix(strings.TrimSpace(existing), ";") {
		sep = "; "
	}
	newText := sep + preludeToAdd

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Add %s to SHELL instruction", preludeToAdd),
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(
					file, shellLine, lastQuote, shellLine, lastQuote,
				),
				NewText: newText,
			},
		},
	}
}

func (r *ErrorActionPreferenceRule) buildShellInsertFix(
	file string,
	sm *sourcemap.SourceMap,
	info *semantic.StageInfo,
	priority int,
	needStop bool,
	needNative bool,
) *rules.SuggestedFix {
	if info.Stage == nil || len(info.Stage.Location) == 0 {
		return nil
	}

	// Insert SHELL after the FROM instruction.
	fromEndLine := info.Stage.Location[len(info.Stage.Location)-1].End.Line
	insertLine := fromEndLine + 1

	executable := shellExecutableForStage(info)
	preludeToAdd := buildPreludeString(needStop, needNative)

	shellInstruction := `SHELL ["` + executable + `", "-Command", "` + preludeToAdd + `"]`

	indent := ""
	if sm != nil && fromEndLine > 0 && fromEndLine <= sm.LineCount() {
		indent = leadingIndentForLine(sm.Line(fromEndLine - 1))
	}

	return &rules.SuggestedFix{
		Description: "Insert PowerShell SHELL instruction with error-handling preferences",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(file, insertLine, 0, insertLine, 0),
				NewText:  indent + shellInstruction + "\n",
			},
		},
	}
}

func shellExecutableForStage(info *semantic.StageInfo) string {
	if info == nil {
		return defaultPwshExecutable
	}
	// Use the executable from the current shell setting if available.
	if len(info.ShellSetting.Shell) > 0 {
		exe := info.ShellSetting.Shell[0]
		norm := shellutil.NormalizeShellExecutableName(exe)
		if norm == "powershell" || norm == defaultPwshExecutable {
			return exe
		}
	}
	// Default for most modern PowerShell images.
	return defaultPwshExecutable
}

func buildPreludeString(needStop, needNative bool) string {
	var parts []string
	if needStop {
		parts = append(parts, "$ErrorActionPreference = 'Stop';")
	}
	if needNative {
		parts = append(parts, "$PSNativeCommandUseErrorActionPreference = $true;")
	}
	return strings.Join(parts, " ")
}

// extractShellLastArg extracts the content of the last quoted argument in a SHELL instruction.
func extractShellLastArg(line string) string {
	// Find the last pair of quotes.
	lastClose := strings.LastIndex(line, `"`)
	if lastClose < 1 {
		return ""
	}
	lastOpen := strings.LastIndex(line[:lastClose], `"`)
	if lastOpen < 0 {
		return ""
	}
	return line[lastOpen+1 : lastClose]
}

func leadingIndentForLine(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

func (r *ErrorActionPreferenceRule) resolveConfig(config any) ErrorActionPreferenceConfig {
	return configutil.Coerce(config, DefaultErrorActionPreferenceConfig())
}

func init() {
	rules.Register(NewErrorActionPreferenceRule())
}
