package labels

import (
	"fmt"
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/sourcemap"
)

// PreferGroupedRuleCode is the full rule code.
const PreferGroupedRuleCode = rules.TallyRulePrefix + "labels/prefer-grouped"

// preferGroupedDefaultMinLabels is the default minimum total label-pair count
// in an adjacent run before the rule reports.
const preferGroupedDefaultMinLabels = 3

// PreferGroupedConfig configures the prefer-grouped rule.
type PreferGroupedConfig struct {
	// MinLabels is the minimum number of label key/value pairs (across
	// adjacent LABEL instructions in the same stage) that triggers the rule.
	// Default is 3.
	MinLabels *int `json:"min-labels,omitempty" koanf:"min-labels"`
}

// DefaultPreferGroupedConfig returns the default configuration.
func DefaultPreferGroupedConfig() PreferGroupedConfig {
	n := preferGroupedDefaultMinLabels
	return PreferGroupedConfig{MinLabels: &n}
}

// PreferGroupedRule flags scattered LABEL instructions that should be combined
// into a single multi-line LABEL block per stage.
type PreferGroupedRule struct {
	schema map[string]any
}

// NewPreferGroupedRule creates a new rule instance.
func NewPreferGroupedRule() *PreferGroupedRule {
	schema, err := configutil.RuleSchema(PreferGroupedRuleCode)
	if err != nil {
		panic(err)
	}
	return &PreferGroupedRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *PreferGroupedRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferGroupedRuleCode,
		Name:            "Prefer grouped LABEL instructions",
		Description:     "Combine adjacent LABEL instructions in the same stage into one multi-line LABEL block",
		DocURL:          rules.TallyDocURL(PreferGroupedRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "style",
		IsExperimental:  false,
		// Priority 96: runs before newline-per-chained-call (97). The merged
		// output uses the same multi-line shape that the splitter emits, so
		// the splitter's idempotent guard skips it on the same fix run.
		FixPriority: 96,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferGroupedRule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *PreferGroupedRule) DefaultConfig() any {
	return DefaultPreferGroupedConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferGroupedRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(PreferGroupedRuleCode, config)
}

// Check runs the rule.
func (r *PreferGroupedRule) Check(input rules.LintInput) []rules.Violation {
	if input.Facts == nil {
		return nil
	}

	cfg := r.resolveConfig(input.Config)
	minLabels := preferGroupedDefaultMinLabels
	if cfg.MinLabels != nil && *cfg.MinLabels >= 2 {
		minLabels = *cfg.MinLabels
	}

	meta := r.Metadata()
	sm := input.SourceMap()
	escapeToken := labelEscapeToken(input)

	var violations []rules.Violation
	for _, stage := range input.Facts.Stages() {
		if stage == nil {
			continue
		}
		for _, run := range adjacentLabelRuns(stage, sm) {
			totalPairs := 0
			for _, inst := range run {
				totalPairs += len(inst.Pairs)
			}
			if len(run) < 2 || totalPairs < minLabels {
				continue
			}

			loc := rules.NewLocationFromRanges(input.File, run[0].Location)
			msg := fmt.Sprintf(
				"%d adjacent LABEL instructions in this stage carry %d label pairs; combine them into one multi-line LABEL",
				len(run), totalPairs,
			)
			violation := rules.NewViolation(loc, meta.Code, msg, meta.DefaultSeverity).
				WithDocURL(meta.DocURL).
				WithDetail("Grouping LABEL pairs into one multi-line block keeps related image metadata together for review and avoids unrelated edits drifting between separate LABEL instructions.")

			if fix := buildPreferGroupedFix(input.File, sm, run, escapeToken, meta); fix != nil {
				violation = violation.WithSuggestedFix(fix)
			}
			violations = append(violations, violation)
		}
	}

	return violations
}

// adjacentLabelRuns returns groups of consecutive LabelInstructionFact entries
// that are not separated by any other Dockerfile instruction or by a comment
// line in the source. Each returned slice is non-empty.
func adjacentLabelRuns(stage *facts.StageFacts, sm *sourcemap.SourceMap) [][]facts.LabelInstructionFact {
	if stage == nil || len(stage.LabelInstructions) == 0 {
		return nil
	}

	var runs [][]facts.LabelInstructionFact
	var current []facts.LabelInstructionFact

	for _, inst := range stage.LabelInstructions {
		if len(current) == 0 {
			current = append(current, inst)
			continue
		}
		prev := current[len(current)-1]
		if !labelInstructionsAdjacent(prev, inst, sm) {
			runs = append(runs, current)
			current = []facts.LabelInstructionFact{inst}
			continue
		}
		current = append(current, inst)
	}
	if len(current) > 0 {
		runs = append(runs, current)
	}
	return runs
}

// labelInstructionsAdjacent reports whether b immediately follows a within the
// same stage with no intervening non-LABEL command and no comment line in the
// source between them.
func labelInstructionsAdjacent(a, b facts.LabelInstructionFact, sm *sourcemap.SourceMap) bool {
	if a.StageIndex != b.StageIndex {
		return false
	}
	if b.CommandIndex != a.CommandIndex+1 {
		// Some other Dockerfile instruction (ARG/ENV/RUN/...) appeared between
		// them, so they do not form a contiguous metadata block.
		return false
	}
	if len(a.Location) == 0 || len(b.Location) == 0 {
		return false
	}
	prevEnd := a.Location[0].End.Line
	nextStart := b.Location[0].Start.Line
	if sm == nil {
		return true
	}
	for line := prevEnd + 1; line < nextStart; line++ {
		raw := sm.Line(line - 1)
		trimmed := strings.TrimLeft(raw, " \t")
		if strings.HasPrefix(trimmed, "#") {
			return false
		}
	}
	return true
}

// buildPreferGroupedFix constructs a safe fix that rewrites the first LABEL
// instruction in the run with a multi-line LABEL containing every pair from
// the run, and deletes the remaining LABEL instructions (including their
// trailing newlines).
//
// The fix is suppressed when the run contains:
//   - any LABEL whose key is dynamic (cannot be rendered statically),
//   - any LABEL with the legacy "key value" form (NoDelim),
//   - any duplicate key inside the run (deferred to no-duplicate-keys), or
//   - a multi-line LABEL whose escape syntax we cannot replicate.
func buildPreferGroupedFix(
	file string,
	sm *sourcemap.SourceMap,
	run []facts.LabelInstructionFact,
	escapeToken rune,
	meta rules.RuleMetadata,
) *rules.SuggestedFix {
	if sm == nil || len(run) < 2 {
		return nil
	}
	if !preferGroupedFixSafe(run) {
		return nil
	}

	first := run[0]
	if len(first.Location) == 0 || first.Command == nil {
		return nil
	}

	startLine := first.Location[0].Start.Line
	endLine := sm.ResolveEndLineWithEscape(first.Location[0].End.Line, escapeToken)
	if startLine <= 0 || endLine < startLine || endLine > sm.LineCount() {
		return nil
	}

	indent := leadingHorizontalWhitespace(sm.Line(startLine - 1))
	merged := renderMergedLabel(run, indent)
	if merged == "" {
		return nil
	}

	edits := make([]rules.TextEdit, 0, len(run))
	edits = append(edits, rules.TextEdit{
		Location: rules.NewRangeLocation(file, startLine, 0, endLine, len(sm.Line(endLine-1))),
		NewText:  merged,
	})

	for _, inst := range run[1:] {
		if len(inst.Location) == 0 {
			return nil
		}
		instStart := inst.Location[0].Start.Line
		instEnd := sm.ResolveEndLineWithEscape(inst.Location[0].End.Line, escapeToken)
		if instStart <= 0 || instEnd < instStart || instEnd > sm.LineCount() {
			return nil
		}
		edits = append(edits, rules.TextEdit{
			Location: deleteInstructionLocation(file, sm, instStart, instEnd),
			NewText:  "",
		})
	}

	return &rules.SuggestedFix{
		Description: fmt.Sprintf("Combine %d adjacent LABEL instructions into one multi-line LABEL", len(run)),
		Safety:      rules.FixSafe,
		Priority:    meta.FixPriority,
		IsPreferred: true,
		Edits:       edits,
	}
}

// preferGroupedFixSafe reports whether the run is safe to rewrite as a single
// merged LABEL instruction.
func preferGroupedFixSafe(run []facts.LabelInstructionFact) bool {
	seen := make(map[string]struct{})
	for _, inst := range run {
		if inst.Command == nil {
			return false
		}
		for _, pair := range inst.Pairs {
			if pair.NoDelim {
				return false
			}
			if pair.KeyIsDynamic {
				return false
			}
			if pair.ExpansionError != "" {
				return false
			}
			if pair.Key == "" {
				return false
			}
			if _, dup := seen[pair.Key]; dup {
				return false
			}
			seen[pair.Key] = struct{}{}
		}
	}
	return true
}

// renderMergedLabel produces the source text for one multi-line LABEL
// instruction containing every pair from run, in source order. The output
// matches the shape that tally/newline-per-chained-call emits, so the splitter
// recognises it as already-formatted on a co-running fix.
func renderMergedLabel(run []facts.LabelInstructionFact, indent string) string {
	var b strings.Builder
	first := true
	for _, inst := range run {
		if inst.Command == nil {
			return ""
		}
		for _, kv := range inst.Command.Labels {
			if first {
				b.WriteString(indent)
				b.WriteString("LABEL ")
				b.WriteString(kv.String())
				first = false
				continue
			}
			b.WriteString(" \\\n")
			b.WriteString(indent)
			b.WriteByte('\t')
			b.WriteString(kv.String())
		}
	}
	if first {
		return ""
	}
	return b.String()
}

// resolveConfig extracts the config from input, falling back to defaults.
func (r *PreferGroupedRule) resolveConfig(config any) PreferGroupedConfig {
	return configutil.Coerce(config, DefaultPreferGroupedConfig())
}

func init() {
	rules.Register(NewPreferGroupedRule())
}
