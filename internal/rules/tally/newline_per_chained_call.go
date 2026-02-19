package tally

import (
	"fmt"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

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
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"min-commands": map[string]any{
				"type":        "integer",
				"minimum":     2,
				"default":     2,
				"description": "Minimum chained commands to trigger chain splitting",
			},
		},
		"additionalProperties": false,
	}
}

// DefaultConfig returns the default configuration for this rule.
func (r *NewlinePerChainedCallRule) DefaultConfig() any {
	return DefaultNewlinePerChainedCallConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *NewlinePerChainedCallRule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
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

	// Check cross-rule coordination with prefer-run-heredoc
	heredocEnabled := input.IsRuleEnabled(rules.HeredocRuleCode)
	heredocMinCommands := input.GetHeredocMinCommands()

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
				if v := r.checkRun(c, shellVariant, input.File, sm, meta, minCommands, heredocEnabled, heredocMinCommands); v != nil {
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
	heredocEnabled bool,
	heredocMinCommands int,
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
			instrIndent, file, minCommands, heredocEnabled, heredocMinCommands,
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

// findMountPositions scans instruction source lines for --mount= tokens.
func findMountPositions(instrLines []string, startLine int) []mountPos {
	var mounts []mountPos
	for i, line := range instrLines {
		dockerLine := startLine + i
		searchFrom := 0
		for {
			idx := strings.Index(line[searchFrom:], "--mount=")
			if idx < 0 {
				break
			}
			col := searchFrom + idx
			// Find end of this mount value (next space or end of line/backslash)
			endCol := col + 8 // skip "--mount="
			for endCol < len(line) {
				ch := line[endCol]
				if ch == ' ' || ch == '\t' || ch == '\\' {
					break
				}
				endCol++
			}
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

	mounts := findMountPositions(instrLines, startLine)
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
	heredocEnabled bool,
	heredocMinCommands int,
) []rules.TextEdit {
	script := getRunScriptFromCmd(run)
	if script == "" {
		return nil
	}

	// Skip if prefer-run-heredoc is enabled and this is a heredoc candidate
	if heredocEnabled && shell.IsHeredocCandidate(script, shellVariant, heredocMinCommands) {
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

	var sameLineBoundaries []shell.ChainBoundary
	for _, b := range boundaries {
		if b.SameLine {
			sameLineBoundaries = append(sameLineBoundaries, b)
		}
	}

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
		// Find end of this flag value
		flagEnd := offset + 2
		for flagEnd < len(firstLine) {
			ch := firstLine[flagEnd]
			if ch == ' ' || ch == '\t' {
				break
			}
			flagEnd++
		}
		offset = flagEnd
		// Skip whitespace after flag
		for offset < len(firstLine) && (firstLine[offset] == ' ' || firstLine[offset] == '\t') {
			offset++
		}
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

	instrLines := make([]string, 0, endLine-startLine+1)
	for l := startLine; l <= endLine; l++ {
		instrLines = append(instrLines, sm.Line(l-1))
	}

	instrIndent := leadingWhitespace(instrLines[0])

	// Find positions of each key in the source text
	var keyPositions []keyPos
	for _, kv := range cmd.Labels {
		pos := findKeyPosition(instrLines, startLine, kv.Key, keyPositions)
		if pos.line > 0 {
			keyPositions = append(keyPositions, pos)
		}
	}

	if len(keyPositions) < 2 {
		return nil
	}

	// Check if multiple keys share a source line
	hasSameLinePairs := false
	for i := 1; i < len(keyPositions); i++ {
		if keyPositions[i].line == keyPositions[i-1].line {
			hasSameLinePairs = true
			break
		}
	}

	if !hasSameLinePairs {
		return nil
	}

	// Generate edits: find the gap between end of previous pair and start of next key
	var edits []rules.TextEdit
	for i := 1; i < len(keyPositions); i++ {
		if keyPositions[i].line != keyPositions[i-1].line {
			continue // already on different lines
		}

		// Find end of previous key=value pair by searching backwards from current key position
		prevLine := keyPositions[i-1].line
		lineText := instrLines[prevLine-startLine]
		gapEnd := keyPositions[i].col

		// Find where the previous value ends (search backwards from gapEnd for whitespace)
		gapStart := gapEnd
		for gapStart > 0 && (lineText[gapStart-1] == ' ' || lineText[gapStart-1] == '\t') {
			gapStart--
		}

		if gapStart < gapEnd {
			edits = append(edits, rules.TextEdit{
				Location: rules.NewRangeLocation(file, prevLine, gapStart, prevLine, gapEnd),
				NewText:  " \\\n" + instrIndent + "\t",
			})
		}
	}

	if len(edits) == 0 {
		return nil
	}

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

// keyPos records the Dockerfile line and column of a LABEL key.
type keyPos struct {
	line int // 1-based Dockerfile line
	col  int // 0-based column of key start
}

// findKeyPosition finds the position of a key in the instruction source lines,
// skipping positions that have already been matched.
func findKeyPosition(instrLines []string, startLine int, key string, alreadyFound []keyPos) keyPos {
	for i, line := range instrLines {
		docLine := startLine + i
		searchFrom := 0
		if i == 0 {
			// Skip LABEL keyword on first line
			upper := strings.ToUpper(strings.TrimLeft(line, " \t"))
			if strings.HasPrefix(upper, "LABEL") {
				searchFrom = strings.Index(strings.ToUpper(line), "LABEL") + 5
				// Skip whitespace after LABEL
				for searchFrom < len(line) && (line[searchFrom] == ' ' || line[searchFrom] == '\t') {
					searchFrom++
				}
			}
		}

		for {
			idx := strings.Index(line[searchFrom:], key)
			if idx < 0 {
				break
			}
			col := searchFrom + idx

			// Verify this is a key position (followed by =)
			afterKey := col + len(key)
			if afterKey < len(line) && line[afterKey] == '=' {
				// Check if already found
				alreadyUsed := false
				for _, af := range alreadyFound {
					if af.line == docLine && af.col == col {
						alreadyUsed = true
						break
					}
				}
				if !alreadyUsed {
					return keyPos{line: docLine, col: col}
				}
			}
			searchFrom = col + 1
		}
	}
	return keyPos{}
}

// checkHealthcheck checks a HEALTHCHECK CMD instruction for chained commands.
func (r *NewlinePerChainedCallRule) checkHealthcheck(
	cmd *instructions.HealthCheckCommand,
	shellVariant shell.Variant,
	file string,
	sm *sourcemap.SourceMap,
	meta rules.RuleMetadata,
	minCommands int,
) *rules.Violation {
	if cmd.Health == nil || len(cmd.Health.Test) < 2 || cmd.Health.Test[0] != "CMD-SHELL" {
		return nil
	}

	script := cmd.Health.Test[1]
	if script == "" {
		return nil
	}

	if len(cmd.Location()) == 0 {
		return nil
	}

	startLine := cmd.Location()[0].Start.Line
	endLine := resolveEndLine(sm, cmd.Location())

	instrLines := make([]string, 0, endLine-startLine+1)
	for l := startLine; l <= endLine; l++ {
		instrLines = append(instrLines, sm.Line(l-1))
	}

	instrIndent := leadingWhitespace(instrLines[0])

	// Find where CMD starts in the source text (after HEALTHCHECK [options] CMD)
	cmdStartCol := findHealthcheckCmdStart(instrLines[0])
	if cmdStartCol < 0 {
		return nil
	}

	edits := r.collectSameLineChainEdits(instrLines, startLine, cmdStartCol, minCommands, shellVariant, instrIndent, file)
	if len(edits) == 0 {
		return nil
	}

	loc := rules.NewLocationFromRanges(file, cmd.Location())
	v := rules.NewViolation(loc, meta.Code, "split chained commands onto separate lines", meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Split HEALTHCHECK CMD chains onto separate continuation lines",
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits:       edits,
			IsPreferred: true,
		})

	return &v
}

// findHealthcheckCmdStart finds the byte offset where the CMD argument starts
// in a HEALTHCHECK line (after HEALTHCHECK [options] CMD).
func findHealthcheckCmdStart(line string) int {
	upper := strings.ToUpper(line)
	// Find "CMD " after HEALTHCHECK
	idx := strings.Index(upper, "HEALTHCHECK")
	if idx < 0 {
		return -1
	}
	rest := line[idx+11:] // skip "HEALTHCHECK"
	cmdIdx := strings.Index(strings.ToUpper(rest), "CMD")
	if cmdIdx < 0 {
		return -1
	}
	// CMD keyword position + 3 for "CMD" + skip whitespace
	pos := idx + 11 + cmdIdx + 3
	for pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
		pos++
	}
	return pos
}

// resolveConfig extracts the config from input, falling back to defaults.
func (r *NewlinePerChainedCallRule) resolveConfig(config any) NewlinePerChainedCallConfig {
	return configutil.Coerce(config, DefaultNewlinePerChainedCallConfig())
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewNewlinePerChainedCallRule())
}
