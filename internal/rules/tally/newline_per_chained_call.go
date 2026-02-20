package tally

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NewlinePerChainedCallRuleCode is the full rule code for the newline-per-chained-call rule.
const NewlinePerChainedCallRuleCode = rules.TallyRulePrefix + "newline-per-chained-call"

// NewlinePerChainedCallConfig is the configuration for the newline-per-chained-call rule.
type NewlinePerChainedCallConfig struct {
	// MinCommands is the minimum number of chained commands to trigger splitting.
	MinCommands *int `json:"min-commands,omitempty" koanf:"min-commands"`
}

// DefaultNewlinePerChainedCallConfig returns the default configuration.
func DefaultNewlinePerChainedCallConfig() NewlinePerChainedCallConfig {
	minCommands := 2
	return NewlinePerChainedCallConfig{
		MinCommands: &minCommands,
	}
}

// NewlinePerChainedCallRule implements the newline-per-chained-call linting rule.
type NewlinePerChainedCallRule struct{}

// NewNewlinePerChainedCallRule creates a new newline-per-chained-call rule instance.
func NewNewlinePerChainedCallRule() *NewlinePerChainedCallRule {
	return &NewlinePerChainedCallRule{}
}

// Metadata returns the rule metadata.
func (r *NewlinePerChainedCallRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NewlinePerChainedCallRuleCode,
		Name:            "Newline Per Chained Call",
		Description:     "Each chained element within an instruction should be on its own line",
		DocURL:          rules.TallyDocURL(NewlinePerChainedCallRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		// Priority 97: must run after all same-line content fixes (DL3027/0,
		// DL3014/10, DL3047/96) whose column shifts the fixer tracks. Our edits
		// insert newlines which the fixer can't track, so we run last among syncs.
		FixPriority: 97,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *NewlinePerChainedCallRule) Schema() map[string]any {
	schema, err := configutil.RuleSchema(NewlinePerChainedCallRuleCode)
	if err != nil {
		panic(err)
	}
	return schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *NewlinePerChainedCallRule) DefaultConfig() any {
	return DefaultNewlinePerChainedCallConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *NewlinePerChainedCallRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(NewlinePerChainedCallRuleCode, config)
}

// Check runs the newline-per-chained-call rule.
func (r *NewlinePerChainedCallRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	meta := r.Metadata()
	sm := input.SourceMap()

	minCommands := 2
	if cfg.MinCommands != nil {
		minCommands = *cfg.MinCommands
	}
	if minCommands < 2 {
		minCommands = 2
	}

	// Get semantic model for shell variant info (may be nil)
	sem, _ := input.Semantic.(*semantic.Model) //nolint:errcheck // Type assertion OK returns false for nil

	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		shellVariant := shell.VariantBash
		if sem != nil {
			if info := sem.StageInfo(stageIdx); info != nil {
				shellVariant = info.ShellSetting.Variant
				if shellVariant.IsNonPOSIX() {
					continue
				}
			}
		}

		for _, cmd := range stage.Commands {
			switch c := cmd.(type) {
			case *instructions.RunCommand:
				if v := r.checkRun(c, shellVariant, input.File, sm, meta, minCommands); v != nil {
					violations = append(violations, *v)
				}
			case *instructions.LabelCommand:
				if v := r.checkLabel(c, input.File, sm, meta); v != nil {
					violations = append(violations, *v)
				}
			case *instructions.HealthCheckCommand:
				if v := r.checkHealthcheck(c, shellVariant, input.File, sm, meta, minCommands); v != nil {
					violations = append(violations, *v)
				}
			}
		}
	}

	return violations
}

// checkRun checks a RUN instruction for chains and mounts that need splitting.
func (r *NewlinePerChainedCallRule) checkRun(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
	minCommands int,
) *rules.Violation {
	if len(run.Location()) == 0 {
		return nil
	}

	startLine := run.Location()[0].Start.Line
	endLine := resolveEndLine(sm, run.Location())

	// Get instruction source lines
	instrLines := make([]string, 0, endLine-startLine+1)
	for l := startLine; l <= endLine; l++ {
		instrLines = append(instrLines, sm.Line(l-1))
	}

	instrIndent := leadingWhitespace(instrLines[0])
	loc := rules.NewLocationFromRanges(file, run.Location())

	var chainEdits []rules.TextEdit

	// --- Mount splitting ---
	mountEdits := r.checkRunMounts(run, instrLines, startLine, instrIndent, file)

	// --- Chain splitting ---
	isHeredocRun := len(run.Files) > 0
	if !isHeredocRun && run.PrependShell {
		chainEdits = r.checkRunChains(
			run, shellVariant, instrLines, startLine,
			instrIndent, file, minCommands,
		)
	}

	if len(chainEdits) == 0 && len(mountEdits) == 0 {
		return nil
	}

	// Combine all edits into a single SuggestedFix
	allEdits := make([]rules.TextEdit, 0, len(mountEdits)+len(chainEdits))
	allEdits = append(allEdits, mountEdits...)
	allEdits = append(allEdits, chainEdits...)

	parts := make([]string, 0, 2)
	if len(mountEdits) > 0 {
		parts = append(parts, "mount flags")
	}
	if len(chainEdits) > 0 {
		parts = append(parts, "chained commands")
	}
	msg := fmt.Sprintf("split %s onto separate lines", strings.Join(parts, " and "))

	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Split onto separate continuation lines",
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits:       allEdits,
			IsPreferred: true,
		})

	return &v
}

// mountPos records the position of a --mount= flag in the Dockerfile.
type mountPos struct {
	line int // 1-based Dockerfile line
	col  int // 0-based byte offset within the line
	end  int // 0-based byte offset of end of this --mount=... token
}

// findMountPositions scans instruction source lines for --mount= tokens in the
// flag area only (before the shell command starts). cmdStartCol is the byte
// offset on the first line where the shell command begins; anything at or after
// that offset is command text and must not be scanned.
func findMountPositions(instrLines []string, startLine, cmdStartCol int) []mountPos {
	var mounts []mountPos
	for i, line := range instrLines {
		dockerLine := startLine + i
		// On the first line, only scan the flag area (before the command).
		scanLimit := len(line)
		if i == 0 && cmdStartCol < scanLimit {
			scanLimit = cmdStartCol
		}
		searchFrom := 0
		for {
			idx := strings.Index(line[searchFrom:scanLimit], "--mount=")
			if idx < 0 {
				break
			}
			col := searchFrom + idx
			// Find end of this mount value, handling quoted values.
			endCol := skipFlagValue(line, col)
			mounts = append(mounts, mountPos{line: dockerLine, col: col, end: endCol})
			searchFrom = endCol
		}
	}
	return mounts
}

// checkRunMounts checks for multiple --mount= flags on the same line.
func (r *NewlinePerChainedCallRule) checkRunMounts(
	run *instructions.RunCommand,
	instrLines []string,
	startLine int,
	instrIndent string,
	file string,
) []rules.TextEdit {
	// FlagsUsed deduplicates — "mount" appears once regardless of count.
	hasMounts := slices.ContainsFunc(run.FlagsUsed, func(f string) bool {
		return strings.HasPrefix(f, "mount")
	})
	if !hasMounts {
		return nil
	}

	cmdStartCol := findCmdStartCol(instrLines[0])
	mounts := findMountPositions(instrLines, startLine, cmdStartCol)
	if len(mounts) < 2 {
		return nil
	}

	// Check if all mounts are already on separate lines
	allSameLine := !slices.ContainsFunc(mounts[1:], func(m mountPos) bool {
		return m.line != mounts[0].line
	})
	if !allSameLine {
		return nil // Already split
	}

	// Generate edits to split mounts (and command after last mount)
	edits := make([]rules.TextEdit, 0, len(mounts))
	mountLine := mounts[0].line
	lineText := instrLines[mountLine-startLine]
	sep := " \\\n" + instrIndent + "\t"

	// Split between consecutive mounts
	for i := range len(mounts) - 1 {
		gapStart := mounts[i].end
		gapEnd := mounts[i+1].col
		if gapStart >= gapEnd {
			continue
		}
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(file, mountLine, gapStart, mountLine, gapEnd),
			NewText:  sep,
		})
	}

	// Split between last mount and the command
	lastMount := mounts[len(mounts)-1]
	afterLastMount := lastMount.end
	if afterLastMount < len(lineText) {
		cmdStart := afterLastMount
		for cmdStart < len(lineText) && (lineText[cmdStart] == ' ' || lineText[cmdStart] == '\t') {
			cmdStart++
		}
		if cmdStart < len(lineText) && lineText[cmdStart] != '\\' {
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(file, mountLine, afterLastMount, mountLine, cmdStart),
				NewText:  sep,
			})
		}
	}

	return edits
}

// checkRunChains checks for chained commands (&&/||) on the same line.
func (r *NewlinePerChainedCallRule) checkRunChains(
	run *instructions.RunCommand,
	shellVariant shell.Variant,
	instrLines []string,
	startLine int,
	instrIndent string,
	file string,
	minCommands int,
) []rules.TextEdit {
	script := getRunScriptFromCmd(run)
	if script == "" {
		return nil
	}

	// Find where the command starts in the first line (after RUN and flags)
	cmdStartCol := findCmdStartCol(instrLines[0])

	return r.collectSameLineChainEdits(instrLines, startLine, cmdStartCol, minCommands, shellVariant, instrIndent, file)
}

// collectSameLineChainEdits reconstructs source text and returns chain-split
// edits for any same-line boundaries, or nil if none are needed.
func (r *NewlinePerChainedCallRule) collectSameLineChainEdits(
	instrLines []string,
	startLine, cmdStartCol, minCommands int,
	shellVariant shell.Variant,
	instrIndent, file string,
) []rules.TextEdit {
	sourceText := shell.ReconstructSourceText(instrLines, cmdStartCol)
	if shell.ScriptHasInlineHeredoc(sourceText, shellVariant) {
		return nil
	}

	boundaries, totalCmds := shell.CollectChainBoundaries(sourceText, shellVariant)
	if totalCmds < minCommands {
		return nil
	}

	sameLineBoundaries := slices.DeleteFunc(boundaries, func(b shell.ChainBoundary) bool {
		return !b.SameLine
	})

	if len(sameLineBoundaries) == 0 {
		return nil
	}

	return r.generateChainEdits(sameLineBoundaries, startLine, cmdStartCol, instrIndent, file)
}

// findCmdStartCol finds the byte offset where the shell command begins
// in the first line of a RUN instruction (after RUN and any --mount/--network flags).
func findCmdStartCol(firstLine string) int {
	// Skip leading whitespace
	trimmed := strings.TrimLeft(firstLine, " \t")
	offset := len(firstLine) - len(trimmed)

	// Skip "RUN" keyword
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "RUN") {
		offset += 3
	}

	rest := firstLine[offset:]

	// Skip whitespace after RUN
	trimmed = strings.TrimLeft(rest, " \t")
	offset += len(rest) - len(trimmed)

	// Skip any flags (--mount=..., --network=..., --security=...)
	for strings.HasPrefix(firstLine[offset:], "--") {
		offset = skipFlagValue(firstLine, offset)
		// Skip whitespace after flag
		for offset < len(firstLine) && (firstLine[offset] == ' ' || firstLine[offset] == '\t') {
			offset++
		}
	}

	return offset
}

// skipFlagValue advances past a Dockerfile flag token (e.g., --mount=type=cache,target=/var)
// starting at offset. Handles double-quoted values (e.g., source="/path with spaces").
func skipFlagValue(line string, offset int) int {
	for offset < len(line) {
		ch := line[offset]
		if ch == '"' {
			// Skip to closing quote.
			offset++
			for offset < len(line) && line[offset] != '"' {
				offset++
			}
			if offset < len(line) {
				offset++ // skip closing quote
			}
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\\' {
			break
		}
		offset++
	}
	return offset
}

// generateChainEdits creates TextEdits for chain boundary splits.
// Source text positions from the shell parser need mapping to Dockerfile coordinates.
func (r *NewlinePerChainedCallRule) generateChainEdits(
	boundaries []shell.ChainBoundary,
	startLine int,
	cmdStartCol int,
	instrIndent string,
	file string,
) []rules.TextEdit {
	edits := make([]rules.TextEdit, 0, len(boundaries))

	for _, b := range boundaries {
		// Map source text lines to Dockerfile lines.
		// Line 1 in source text = startLine in Dockerfile.
		leftDocLine := startLine + b.LeftEndLine - 1
		rightDocLine := startLine + b.RightStartLine - 1

		// Column mapping: line 1 of source text starts at cmdStartCol;
		// continuation lines start at column 0 (after leading whitespace is
		// part of the text).
		leftDocCol := b.LeftEndCol - 1 // 1-based to 0-based
		if b.LeftEndLine == 1 {
			leftDocCol += cmdStartCol
		}
		rightDocCol := b.RightStartCol - 1 // 1-based to 0-based
		if b.RightStartLine == 1 {
			rightDocCol += cmdStartCol
		}

		newText := " \\\n" + instrIndent + "\t" + b.Op + " "
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(file, leftDocLine, leftDocCol, rightDocLine, rightDocCol),
			NewText:  newText,
		})
	}

	return edits
}

// checkLabel checks a LABEL instruction for multiple pairs on the same line.
func (r *NewlinePerChainedCallRule) checkLabel(
	cmd *instructions.LabelCommand,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
) *rules.Violation {
	if len(cmd.Labels) <= 1 || len(cmd.Location()) == 0 {
		return nil
	}

	// Skip legacy format (NoDelim) — that's LegacyKeyValueFormat's domain
	for _, kv := range cmd.Labels {
		if kv.NoDelim {
			return nil
		}
	}

	startLine := cmd.Location()[0].Start.Line
	endLine := resolveEndLine(sm, cmd.Location())
	numSourceLines := endLine - startLine + 1

	// Already formatted: first line has LABEL + first pair, each remaining pair
	// gets its own continuation line → need at least len(labels) lines.
	if numSourceLines >= len(cmd.Labels) {
		return nil
	}

	instrIndent := leadingWhitespace(sm.Line(startLine - 1))

	// Pretty-print: replace entire instruction with one pair per line.
	var b strings.Builder
	b.WriteString(instrIndent + "LABEL " + cmd.Labels[0].String())
	for _, kv := range cmd.Labels[1:] {
		b.WriteString(" \\\n" + instrIndent + "\t" + kv.String())
	}

	edits := []rules.TextEdit{{
		Location: rules.NewRangeLocation(file, startLine, 0, endLine, len(sm.Line(endLine-1))),
		NewText:  b.String(),
	}}

	loc := rules.NewLocationFromRanges(file, cmd.Location())
	v := rules.NewViolation(loc, meta.Code, "split LABEL key=value pairs onto separate lines", meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Split LABEL pairs onto separate continuation lines",
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits:       edits,
			IsPreferred: true,
		})

	return &v
}

// checkHealthcheck checks a HEALTHCHECK instruction for flags and/or chained
// commands that should be split onto separate continuation lines.
// Uses whole-instruction replacement (like LABEL) instead of boundary edits.
func (r *NewlinePerChainedCallRule) checkHealthcheck(
	cmd *instructions.HealthCheckCommand,
	shellVariant shell.Variant,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
	minCommands int,
) *rules.Violation {
	if cmd.Health == nil || len(cmd.Health.Test) == 0 || len(cmd.Location()) == 0 {
		return nil
	}

	testMode := cmd.Health.Test[0]
	if testMode == "NONE" || testMode == "" {
		return nil
	}

	startLine := cmd.Location()[0].Start.Line
	endLine := resolveEndLine(sm, cmd.Location())
	instrIndent := leadingWhitespace(sm.Line(startLine - 1))

	// Collect flags from parsed Health config.
	flags := healthcheckFlags(cmd.Health)

	// Format the CMD portion.
	var cmdText string
	numCmdLines := 1 // how many continuation lines the CMD portion occupies

	switch {
	case testMode == "CMD-SHELL" && len(cmd.Health.Test) >= 2:
		script := cmd.Health.Test[1]
		if script == "" {
			return nil
		}
		if !shell.ScriptHasInlineHeredoc(script, shellVariant) {
			_, maxChainCmds := shell.CollectChainBoundaries(script, shellVariant)
			if maxChainCmds >= minCommands {
				cmdText = shell.FormatChainedScript(script, shellVariant)
				numCmdLines = strings.Count(cmdText, "\n") + 1
			} else {
				cmdText = strings.TrimSpace(script)
			}
		} else {
			cmdText = strings.TrimSpace(script)
		}
	case testMode == "CMD" && len(cmd.Health.Test) >= 2:
		// Exec form: reconstruct JSON array.
		cmdText = formatExecArgs(cmd.Health.Test[1:])
	default:
		return nil
	}

	// Total elements: each flag + CMD lines.
	numElements := len(flags) + numCmdLines
	if numElements <= 1 {
		return nil
	}

	numSourceLines := endLine - startLine + 1
	if numSourceLines >= numElements {
		return nil // already properly formatted
	}

	// Pretty-print: replace entire instruction.
	sep := " \\\n" + instrIndent + "\t"

	// Adjust printer's tab indentation to include instruction-level indent.
	if instrIndent != "" {
		cmdText = strings.ReplaceAll(cmdText, "\n\t", "\n"+instrIndent+"\t")
	}

	var b strings.Builder
	b.WriteString(instrIndent + "HEALTHCHECK")

	isFirst := true
	for _, flag := range flags {
		if isFirst {
			b.WriteString(" " + flag)
			isFirst = false
		} else {
			b.WriteString(sep + flag)
		}
	}

	if isFirst {
		b.WriteString(" CMD " + cmdText)
	} else {
		b.WriteString(sep + "CMD " + cmdText)
	}

	edits := []rules.TextEdit{{
		Location: rules.NewRangeLocation(file, startLine, 0, endLine, len(sm.Line(endLine-1))),
		NewText:  b.String(),
	}}

	msg := "split HEALTHCHECK onto separate continuation lines"
	loc := rules.NewLocationFromRanges(file, cmd.Location())
	v := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Split HEALTHCHECK onto separate continuation lines",
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits:       edits,
			IsPreferred: true,
		})

	return &v
}

// healthcheckFlags returns the Dockerfile flag strings for non-zero HEALTHCHECK options.
func healthcheckFlags(h *dockerspec.HealthcheckConfig) []string {
	var flags []string
	if h.Interval != 0 {
		flags = append(flags, "--interval="+formatHealthcheckDuration(h.Interval))
	}
	if h.Timeout != 0 {
		flags = append(flags, "--timeout="+formatHealthcheckDuration(h.Timeout))
	}
	if h.StartPeriod != 0 {
		flags = append(flags, "--start-period="+formatHealthcheckDuration(h.StartPeriod))
	}
	if h.StartInterval != 0 {
		flags = append(flags, "--start-interval="+formatHealthcheckDuration(h.StartInterval))
	}
	if h.Retries != 0 {
		flags = append(flags, fmt.Sprintf("--retries=%d", h.Retries))
	}
	return flags
}

// formatHealthcheckDuration formats a duration for Dockerfile HEALTHCHECK flags.
// Produces clean output like "30s", "1m", "1h" instead of Go's "1m0s", "1h0m0s".
func formatHealthcheckDuration(d time.Duration) string {
	s := d.String()
	// Only trim zero components: "m0s" means zero seconds after minutes,
	// "h0m" means zero minutes after hours. Avoid trimming "30s" → "3".
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

// formatExecArgs formats a CMD exec-form argument list as a JSON array string.
func formatExecArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = strconv.Quote(arg)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// resolveConfig extracts the config from input, falling back to defaults.
func (r *NewlinePerChainedCallRule) resolveConfig(config any) NewlinePerChainedCallConfig {
	return configutil.Coerce(config, DefaultNewlinePerChainedCallConfig())
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewNewlinePerChainedCallRule())
}
