package powershell

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/semantic"
	shellutil "github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// ProgressPreferenceRuleCode is the full rule code.
const ProgressPreferenceRuleCode = rules.TallyRulePrefix + "powershell/progress-preference"

const progressPreferenceAssignment = "$ProgressPreference = 'SilentlyContinue';"

// ProgressPreferenceRule warns when PowerShell RUN instructions invoke
// Invoke-WebRequest (or its alias iwr) without setting
// $ProgressPreference = 'SilentlyContinue'. The per-response progress bars
// tank build throughput on Windows containers.
type ProgressPreferenceRule struct{}

// NewProgressPreferenceRule creates a new rule instance.
func NewProgressPreferenceRule() *ProgressPreferenceRule {
	return &ProgressPreferenceRule{}
}

// Metadata returns the rule metadata.
func (r *ProgressPreferenceRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            ProgressPreferenceRuleCode,
		Name:            "Suppress PowerShell progress bars for web downloads",
		Description:     "PowerShell RUN using Invoke-WebRequest should set $ProgressPreference = 'SilentlyContinue'",
		DocURL:          rules.TallyDocURL(ProgressPreferenceRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		FixPriority:     97, //nolint:mnd // After error-action-preference (96), before prefer-run-heredoc (100).
	}
}

// Check runs the rule.
func (r *ProgressPreferenceRule) Check(input rules.LintInput) []rules.Violation {
	meta := r.Metadata()
	sm := input.SourceMap()

	violations := make([]rules.Violation, 0, len(input.Stages))
	for stageIdx, stage := range input.Stages {
		violations = append(violations, r.checkStage(input, sm, stageIdx, stage, meta)...)
	}
	return violations
}

// progressCheckParams bundles per-RUN inputs shared across the Path 1/2/3 helpers.
type progressCheckParams struct {
	file             string
	sm               *sourcemap.SourceMap
	meta             rules.RuleMetadata
	stageIdx         int
	info             *semantic.StageInfo
	activeShellCmd   []string
	activeShellInstr *instructions.ShellCommand
}

func (r *ProgressPreferenceRule) checkStage(
	input rules.LintInput,
	sm *sourcemap.SourceMap,
	stageIdx int,
	stage instructions.Stage,
	meta rules.RuleMetadata,
) []rules.Violation {
	p := progressCheckParams{
		file:           input.File,
		sm:             sm,
		meta:           meta,
		stageIdx:       stageIdx,
		info:           stageInfoForIndex(input.Semantic, stageIdx),
		activeShellCmd: initialStageShellCmd(input.Semantic, stageIdx),
	}
	stageFixEmitted := false

	violations := make([]rules.Violation, 0, len(stage.Commands))
	for _, cmd := range stage.Commands {
		if sc, ok := cmd.(*instructions.ShellCommand); ok && len(sc.Shell) > 0 {
			p.activeShellCmd = sc.Shell
			p.activeShellInstr = sc
			stageFixEmitted = false
			continue
		}

		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell || len(run.CmdLine) == 0 {
			continue
		}

		v := r.checkRun(p, run)
		if v == nil {
			continue
		}

		// Suppress duplicate SHELL-level fixes within the same SHELL scope.
		// Wrapper-body and heredoc-body fixes are per-RUN and never suppressed;
		// only the Path 3 (variant.IsPowerShell()) violationForPowerShellRun
		// path emits SHELL-modify / SHELL-insert edits that could overlap.
		if v.SuggestedFix != nil && isStageLevelPowerShellFix(p, run) {
			if stageFixEmitted {
				v.SuggestedFix = nil
			} else {
				stageFixEmitted = true
			}
		}
		violations = append(violations, *v)
	}
	return violations
}

func (r *ProgressPreferenceRule) checkRun(p progressCheckParams, run *instructions.RunCommand) *rules.Violation {
	// Determine the effective variant at this RUN line so we can route the fix.
	variant := shellutil.VariantFromShellCmd(p.activeShellCmd)
	if p.info != nil && len(run.Location()) > 0 {
		variant = p.info.ShellVariantAtLine(run.Location()[0].Start.Line)
	}

	// Path 1: heredoc body. Detection and fix both operate on the body.
	if len(run.Files) > 0 && run.Files[0].Data != "" {
		body := run.Files[0].Data
		if !scriptUsesInvokeWebRequest(body) {
			return nil
		}
		if progressPreferenceSet(body) || shellArgsHaveProgressPreference(p.activeShellCmd) {
			return nil
		}
		return r.violationForHeredoc(p, run, variant)
	}

	script := run.CmdLine[0]

	// Path 2: explicit powershell/pwsh -Command wrapper inside a non-PowerShell shell.
	if !variant.IsPowerShell() {
		invocation, ok := parseExplicitPowerShellInvocation(script)
		if !ok {
			return nil
		}
		if !scriptUsesInvokeWebRequest(invocation.script) {
			return nil
		}
		if progressPreferenceSet(invocation.script) {
			return nil
		}
		return r.violationForWrapper(p, run, script, invocation)
	}

	// Path 3: stage shell is PowerShell. SHELL + script both contribute to the prelude.
	if !scriptUsesInvokeWebRequest(script) {
		return nil
	}
	if shellArgsHaveProgressPreference(p.activeShellCmd) || progressPreferenceSet(script) {
		return nil
	}
	return r.violationForPowerShellRun(p, run)
}

// violationForPowerShellRun handles a RUN whose active shell is PowerShell.
// The fix modifies an existing SHELL instruction (if any) or inserts a new one.
func (r *ProgressPreferenceRule) violationForPowerShellRun(p progressCheckParams, run *instructions.RunCommand) *rules.Violation {
	loc := rules.NewLocationFromRanges(p.file, run.Location())
	if loc.IsFileLevel() {
		return nil
	}

	v := newProgressPreferenceViolation(loc, p.meta, p.stageIdx)

	var fix *rules.SuggestedFix
	if p.activeShellInstr != nil {
		fix = buildShellLineFix(p.file, p.sm, p.activeShellInstr, p.meta.FixPriority)
	} else {
		fix = buildShellInsertFix(p.file, p.sm, p.info, p.meta.FixPriority)
	}
	if fix != nil {
		v = v.WithSuggestedFix(fix)
	}
	return &v
}

// violationForWrapper handles an explicit powershell/pwsh -Command wrapper
// inside a non-PowerShell stage. The fix is a zero-width insertion at the
// start of the inner script.
func (r *ProgressPreferenceRule) violationForWrapper(
	p progressCheckParams,
	run *instructions.RunCommand,
	script string,
	invocation explicitPowerShellInvocation,
) *rules.Violation {
	loc := rules.NewLocationFromRanges(p.file, run.Location())
	if loc.IsFileLevel() {
		return nil
	}

	v := newProgressPreferenceViolation(loc, p.meta, p.stageIdx)
	if fix := buildWrapperFix(p.file, p.sm, run, script, invocation, p.meta.FixPriority); fix != nil {
		v = v.WithSuggestedFix(fix)
	}
	return &v
}

// violationForHeredoc handles a RUN heredoc body containing Invoke-WebRequest.
// The fix inserts the assignment at the start of the heredoc body. Only
// emitted when the stage's active variant is PowerShell; otherwise we warn
// without a fix since injecting PowerShell syntax into e.g. a bash heredoc
// would change behavior.
func (r *ProgressPreferenceRule) violationForHeredoc(
	p progressCheckParams,
	run *instructions.RunCommand,
	variant shellutil.Variant,
) *rules.Violation {
	loc := rules.NewLocationFromRanges(p.file, run.Location())
	if loc.IsFileLevel() {
		return nil
	}

	v := newProgressPreferenceViolation(loc, p.meta, p.stageIdx)
	if variant.IsPowerShell() {
		if fix := buildHeredocBodyFix(p.file, p.sm, run, p.meta.FixPriority); fix != nil {
			v = v.WithSuggestedFix(fix)
		}
	}
	return &v
}

func newProgressPreferenceViolation(loc rules.Location, meta rules.RuleMetadata, stageIdx int) rules.Violation {
	msg := "PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue'"
	detail := "PowerShell renders a per-response progress bar for Invoke-WebRequest by default. " +
		"On Windows containers this collapses download throughput by an order of magnitude. " +
		"Set $ProgressPreference = 'SilentlyContinue' before the call."
	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithDetail(detail)
	v.StageIndex = stageIdx
	return v
}

// scriptUsesInvokeWebRequest reports whether the script contains an
// Invoke-WebRequest or iwr command per the PowerShell tree-sitter parser.
func scriptUsesInvokeWebRequest(script string) bool {
	return len(shellutil.FindCommands(script, shellutil.VariantPowerShell, "invoke-webrequest", "iwr")) > 0
}

// progressPreferenceSet walks the leading PowerShell assignments in a script
// and returns true if any one of them sets $ProgressPreference to a value
// that contains "silentlycontinue" (case-insensitive, quotes stripped).
// The walk stops at the first non-assignment so preferences set AFTER the
// IWR call don't count — the risky command already ran before they applied.
func progressPreferenceSet(script string) bool {
	for _, stmt := range shellutil.ExtractChainedCommands(script, shellutil.VariantPowerShell) {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		name, value, ok := shellutil.PowerShellAssignment(trimmed)
		if !ok {
			return false
		}
		if !strings.EqualFold(name, "$ProgressPreference") {
			continue
		}
		if strings.EqualFold(shellutil.DropQuotes(value), "SilentlyContinue") {
			return true
		}
	}
	return false
}

// shellArgsHaveProgressPreference checks whether the last SHELL arg sets
// $ProgressPreference = 'SilentlyContinue' in its prelude.
func shellArgsHaveProgressPreference(shellCmd []string) bool {
	if len(shellCmd) <= 1 {
		return false
	}
	return progressPreferenceSet(shellCmd[len(shellCmd)-1])
}

// buildShellLineFix inserts $ProgressPreference = 'SilentlyContinue' into the
// active SHELL instruction. The edit is a zero-width insertion positioned
// either before the closing `]` (when the last arg is bare "-Command") or
// before the last `"` (when the last arg already carries a prelude).
//
// When this rule fires alongside tally/powershell/error-action-preference on
// the same SHELL line, both rules emit zero-width insertions before the
// closing `]`. The conflict resolver stacks them rather than picking a
// winner, so the final SHELL carries two trailing array elements (one per
// rule) rather than a single consolidated -Command string. That is
// semantically equivalent under pwsh -Command semantics — multiple trailing
// string arguments are space-joined before parsing — but visually verbose.
// tally/powershell/prefer-shell-instruction (priority 95) side-steps this by
// building the entire SHELL in one edit with the full prelude inlined.
func buildShellLineFix(
	file string,
	sm *sourcemap.SourceMap,
	shellCmd *instructions.ShellCommand,
	priority int,
) *rules.SuggestedFix {
	if sm == nil || len(shellCmd.Location()) == 0 {
		return nil
	}
	shellLine := shellCmd.Location()[0].Start.Line
	if shellLine <= 0 {
		return nil
	}

	// Bail out on multi-line SHELL instructions (backslash/backtick
	// continuations). The bracket/quote search below only scans the first
	// physical line, so it can land inside a comment or earlier token and
	// produce an invalid edit. A multi-line SHELL is rare in practice; we
	// just report the violation without a fix.
	if isMultiLineInstruction(sm, shellCmd.Location()[0]) {
		return nil
	}

	sourceLine := sm.Line(shellLine - 1)
	if sourceLine == "" {
		return nil
	}

	existing := extractShellLastArg(sourceLine)
	if strings.EqualFold(strings.TrimSpace(existing), "-Command") {
		closeBracket := strings.LastIndex(sourceLine, "]")
		if closeBracket < 0 {
			return nil
		}
		return &rules.SuggestedFix{
			Description: "Add $ProgressPreference = 'SilentlyContinue' to SHELL instruction",
			Safety:      rules.FixSuggestion,
			Priority:    priority,
			Edits: []rules.TextEdit{
				{
					Location: rules.NewRangeLocation(
						file, shellLine, closeBracket, shellLine, closeBracket,
					),
					NewText: `, "` + progressPreferenceAssignment + `"`,
				},
			},
		}
	}

	lastQuote := strings.LastIndex(sourceLine, `"`)
	if lastQuote < 0 {
		return nil
	}
	sep := " "
	if !strings.HasSuffix(strings.TrimSpace(existing), ";") {
		sep = "; "
	}
	return &rules.SuggestedFix{
		Description: "Add $ProgressPreference = 'SilentlyContinue' to SHELL instruction",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(
					file, shellLine, lastQuote, shellLine, lastQuote,
				),
				NewText: sep + progressPreferenceAssignment,
			},
		},
	}
}

// buildShellInsertFix emits a new SHELL instruction carrying the preference
// after the FROM line. Used when a PowerShell-by-default stage has no
// SHELL instruction preceding the RUN.
func buildShellInsertFix(
	file string,
	sm *sourcemap.SourceMap,
	info *semantic.StageInfo,
	priority int,
) *rules.SuggestedFix {
	if info == nil || info.Stage == nil || len(info.Stage.Location) == 0 {
		return nil
	}

	fromEndLine := info.Stage.Location[len(info.Stage.Location)-1].End.Line
	insertLine := fromEndLine + 1

	executable := shellExecutableForStage(info)

	indent := ""
	if sm != nil && fromEndLine > 0 && fromEndLine <= sm.LineCount() {
		indent = leadingIndentForLine(sm.Line(fromEndLine - 1))
	}

	shellArray := formatShellArray([]string{executable, "-Command", progressPreferenceAssignment})
	if shellArray == "" {
		return nil
	}
	return &rules.SuggestedFix{
		Description: "Insert a PowerShell SHELL instruction with $ProgressPreference = 'SilentlyContinue'",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(file, insertLine, 0, insertLine, 0),
				NewText:  indent + "SHELL " + shellArray + "\n",
			},
		},
	}
}

// buildWrapperFix inserts the assignment at the start of the inner script
// of an explicit powershell/pwsh -Command wrapper. Zero-width insertion.
func buildWrapperFix(
	file string,
	sm *sourcemap.SourceMap,
	run *instructions.RunCommand,
	_ string,
	invocation explicitPowerShellInvocation,
	priority int,
) *rules.SuggestedFix {
	insertLine, insertCol, ok := wrapperInsertionPoint(sm, run, invocation)
	if !ok {
		return nil
	}
	startLine := run.Location()[0].Start.Line

	return &rules.SuggestedFix{
		Description: "Add $ProgressPreference = 'SilentlyContinue' to wrapper script",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(
					file,
					startLine+insertLine, insertCol,
					startLine+insertLine, insertCol,
				),
				NewText: progressPreferenceAssignment + " ",
			},
		},
	}
}

// buildHeredocBodyFix inserts the assignment on its own line at the start of
// the heredoc body. The heredoc body begins on the line after the `<<EOF`
// marker, so we anchor the edit at column 0 of run.Files[0] start line.
func buildHeredocBodyFix(
	file string,
	sm *sourcemap.SourceMap,
	run *instructions.RunCommand,
	priority int,
) *rules.SuggestedFix {
	if sm == nil || len(run.Files) == 0 || len(run.Location()) == 0 {
		return nil
	}

	// BuildKit does not expose the heredoc body's line range directly on
	// run.Files. The body starts on the line after the RUN's first location
	// range end. This holds for single-line heredoc preambles like
	// `RUN <<EOF`, which is the common shape.
	bodyStartLine := run.Location()[0].End.Line + 1
	if bodyStartLine <= 0 || bodyStartLine > sm.LineCount() {
		return nil
	}

	return &rules.SuggestedFix{
		Description: "Add $ProgressPreference = 'SilentlyContinue' to heredoc body",
		Safety:      rules.FixSuggestion,
		Priority:    priority,
		Edits: []rules.TextEdit{
			{
				Location: rules.NewRangeLocation(file, bodyStartLine, 0, bodyStartLine, 0),
				NewText:  progressPreferenceAssignment + "\n",
			},
		},
	}
}

// isStageLevelPowerShellFix reports whether the given RUN would receive a
// stage-level SHELL fix (modify existing SHELL, or insert a new SHELL after
// FROM). Those fixes target the same region across every RUN in the same
// SHELL scope, so only the first should carry the edit — subsequent
// violations in the scope report without a fix to avoid overlapping edits.
//
// The predicate mirrors the Path-3 branch in checkRun: active shell is
// PowerShell and the RUN doesn't route through the wrapper or heredoc paths
// (which produce per-RUN, non-overlapping edits).
func isStageLevelPowerShellFix(p progressCheckParams, run *instructions.RunCommand) bool {
	if len(run.Files) > 0 && run.Files[0].Data != "" {
		// Heredoc fix targets the body of this specific RUN — per-RUN,
		// never overlaps another RUN's fix.
		return false
	}
	variant := shellutil.VariantFromShellCmd(p.activeShellCmd)
	if p.info != nil && len(run.Location()) > 0 {
		variant = p.info.ShellVariantAtLine(run.Location()[0].Start.Line)
	}
	// Only the PowerShell-variant path emits SHELL-modify or SHELL-insert
	// edits. Wrapper fixes (non-PowerShell variant with explicit
	// powershell/pwsh -Command) target the inner script of a specific RUN.
	return variant.IsPowerShell()
}

func init() {
	rules.Register(NewProgressPreferenceRule())
}
