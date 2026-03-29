package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/sourcemap"
)

// NoMultipleEmptyLinesRuleCode is the full rule code for the no-multiple-empty-lines rule.
const NoMultipleEmptyLinesRuleCode = rules.TallyRulePrefix + "no-multiple-empty-lines"

// NoMultipleEmptyLinesConfig is the configuration for the no-multiple-empty-lines rule.
type NoMultipleEmptyLinesConfig struct {
	// Max is the maximum number of consecutive empty lines allowed (nil = use default).
	Max *int `json:"max,omitempty"`

	// MaxBOF is the maximum number of consecutive empty lines at the beginning of the file.
	MaxBOF *int `json:"max-bof,omitempty" koanf:"max-bof"`

	// MaxEOF is the maximum number of consecutive empty lines at the end of the file.
	MaxEOF *int `json:"max-eof,omitempty" koanf:"max-eof"`
}

// DefaultNoMultipleEmptyLinesConfig returns the default configuration.
func DefaultNoMultipleEmptyLinesConfig() NoMultipleEmptyLinesConfig {
	maxVal := 1
	maxBOF := 0
	maxEOF := 0
	return NoMultipleEmptyLinesConfig{
		Max:    &maxVal,
		MaxBOF: &maxBOF,
		MaxEOF: &maxEOF,
	}
}

// resolvedEmptyLinesConfig holds resolved configuration values with defaults applied.
type resolvedEmptyLinesConfig struct {
	maxGeneral int
	maxBOF     int
	maxEOF     int
}

func resolveEmptyLinesLimits(cfg NoMultipleEmptyLinesConfig) resolvedEmptyLinesConfig {
	rc := resolvedEmptyLinesConfig{maxGeneral: 1}
	if cfg.Max != nil {
		rc.maxGeneral = *cfg.Max
	}
	if cfg.MaxBOF != nil {
		rc.maxBOF = *cfg.MaxBOF
	}
	if cfg.MaxEOF != nil {
		rc.maxEOF = *cfg.MaxEOF
	}
	return rc
}

// NoMultipleEmptyLinesRule implements the no-multiple-empty-lines linting rule.
type NoMultipleEmptyLinesRule struct {
	schema map[string]any
}

// NewNoMultipleEmptyLinesRule creates a new no-multiple-empty-lines rule instance.
func NewNoMultipleEmptyLinesRule() *NoMultipleEmptyLinesRule {
	schema, err := configutil.RuleSchema(NoMultipleEmptyLinesRuleCode)
	if err != nil {
		panic(err)
	}
	return &NoMultipleEmptyLinesRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *NoMultipleEmptyLinesRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoMultipleEmptyLinesRuleCode,
		Name:            "No Multiple Empty Lines",
		Description:     "Disallows multiple consecutive empty lines in Dockerfiles",
		DocURL:          rules.TallyDocURL(NoMultipleEmptyLinesRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		FixPriority:     98, // After newline-per-chained-call (97) to avoid line-shift conflicts
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *NoMultipleEmptyLinesRule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *NoMultipleEmptyLinesRule) DefaultConfig() any {
	return DefaultNoMultipleEmptyLinesConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *NoMultipleEmptyLinesRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(NoMultipleEmptyLinesRuleCode, config)
}

// Check runs the no-multiple-empty-lines rule.
func (r *NoMultipleEmptyLinesRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	meta := r.Metadata()
	sm := input.SourceMap()
	rc := resolveEmptyLinesLimits(cfg)
	skipLines := buildSkipLinesForEmptyLines(input, sm)

	lines := sm.Lines()
	lineCount := effectiveLineCount(lines, input.Source)
	if lineCount == 0 {
		return nil
	}

	var violations []rules.Violation

	i := 0
	for i < lineCount {
		if !isEmptyLine(lines[i]) || skipLines[i] {
			i++
			continue
		}

		runStart := i
		for i < lineCount && isEmptyLine(lines[i]) && !skipLines[i] {
			i++
		}
		runEnd := i - 1 // inclusive, 0-based

		if v, ok := checkBlankRun(
			input.File, lines, lineCount, runStart, runEnd, rc, meta,
		); ok {
			violations = append(violations, v)
		}
	}

	return violations
}

// effectiveLineCount returns the number of real content lines, excluding the
// trailing empty element that bytes.Split produces when source ends with \n.
func effectiveLineCount(lines []string, source []byte) int {
	n := len(lines)
	if n > 0 && lines[n-1] == "" && len(source) > 0 && source[len(source)-1] == '\n' {
		n--
	}
	return n
}

// checkBlankRun evaluates a contiguous run of blank lines and returns a
// violation if the run exceeds the applicable limit.
func checkBlankRun(
	file string,
	lines []string,
	lineCount, runStart, runEnd int,
	rc resolvedEmptyLinesConfig,
	meta rules.RuleMetadata,
) (rules.Violation, bool) {
	runLen := runEnd - runStart + 1

	isBOF := runStart == 0
	isEOF := runEnd == lineCount-1
	limit := rc.maxGeneral
	var region string
	switch {
	case isBOF:
		limit = rc.maxBOF
		region = "beginning of file"
	case isEOF:
		limit = rc.maxEOF
		region = "end of file"
	}

	if runLen <= limit {
		return rules.Violation{}, false
	}

	excess := runLen - limit
	excessStart := runStart + limit
	excessStartLine := excessStart + 1 // 1-based

	var message string
	if region != "" {
		message = fmt.Sprintf(
			"too many blank lines at %s (%d), maximum allowed is %d",
			region, runLen, limit,
		)
	} else {
		message = fmt.Sprintf(
			"too many blank lines (%d), maximum allowed is %d",
			runLen, rc.maxGeneral,
		)
	}

	editLoc := buildDeleteEdit(file, lines, lineCount, excessStart, runEnd, isEOF)
	violationLoc := rules.NewRangeLocation(file, excessStartLine, 0, excessStartLine, 0)

	v := rules.NewViolation(violationLoc, meta.Code, message, meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: fmt.Sprintf("Remove %d excess blank line(s)", excess),
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits: []rules.TextEdit{
				{Location: editLoc, NewText: ""},
			},
			IsPreferred: true,
		})

	return v, true
}

// buildDeleteEdit creates a Location covering the excess blank lines to delete.
func buildDeleteEdit(
	file string, lines []string, lineCount, excessStart, runEnd int, isEOF bool,
) rules.Location {
	excessStartLine := excessStart + 1 // 1-based

	if isEOF {
		if excessStart > 0 {
			prevLine := lines[excessStart-1]
			return rules.NewRangeLocation(
				file, excessStartLine-1, len(prevLine),
				lineCount, len(lines[lineCount-1]),
			)
		}
		return rules.NewRangeLocation(file, 1, 0, lineCount, len(lines[lineCount-1]))
	}

	afterRunLine := runEnd + 2 // 1-based line after the run
	return rules.NewRangeLocation(file, excessStartLine, 0, afterRunLine, 0)
}

// isEmptyLine returns true if the line is blank (only whitespace).
func isEmptyLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

// buildSkipLinesForEmptyLines builds a set of line indices (0-based) that should be
// skipped by this rule. We skip:
//   - COPY heredoc bodies (opaque file content)
//   - RUN heredoc bodies when the shell variant has no parser support
func buildSkipLinesForEmptyLines(input rules.LintInput, sm *sourcemap.SourceMap) map[int]bool {
	skip := make(map[int]bool)

	var sem = input.Semantic

	for _, node := range input.AST.AST.Children {
		if len(node.Heredocs) == 0 {
			continue
		}

		instrName := strings.ToLower(node.Value)
		endLine := sm.ResolveEndLine(node.EndLine)

		if instrName == command.Copy {
			for l := node.StartLine; l <= endLine; l++ {
				skip[l-1] = true
			}
			continue
		}

		if instrName == command.Run {
			parseable := false
			if sem != nil {
				parseable = isHeredocParseable(sem, input.Stages, node.StartLine, sm)
			}
			if !parseable {
				for l := node.StartLine; l <= endLine; l++ {
					skip[l-1] = true
				}
			}
		}
	}

	return skip
}

// isHeredocParseable checks if a RUN heredoc at the given line has parser support.
func isHeredocParseable(
	sem *semantic.Model, stages []instructions.Stage, runLine int, sm *sourcemap.SourceMap,
) bool {
	stageIdx := -1
	for i, stage := range stages {
		if len(stage.Location) > 0 && stage.Location[0].Start.Line <= runLine {
			stageIdx = i
		}
	}
	if stageIdx < 0 {
		return false
	}

	info := sem.StageInfo(stageIdx)
	if info == nil {
		return false
	}

	for _, override := range info.HeredocShellOverrides {
		if override.Line == runLine {
			return override.Variant.HasParser()
		}
	}

	if hasUnknownShebang(sm, runLine) {
		return false
	}

	return info.ShellSetting.Variant.HasParser()
}

// hasUnknownShebang checks if the heredoc body starting after runLine has a
// shebang line (#!/...) that wasn't recognized as a known shell.
func hasUnknownShebang(sm *sourcemap.SourceMap, runLine int) bool {
	bodyStart := runLine // 1-based; line after RUN is runLine+1 in 1-based = runLine in 0-based
	if bodyStart >= sm.LineCount() {
		return false
	}
	firstBodyLine := sm.Line(bodyStart) // 0-based
	return strings.HasPrefix(strings.TrimSpace(firstBodyLine), "#!")
}

// resolveConfig extracts the config from input, falling back to defaults.
func (r *NoMultipleEmptyLinesRule) resolveConfig(config any) NoMultipleEmptyLinesConfig {
	if v, ok := config.(int); ok {
		defaults := DefaultNoMultipleEmptyLinesConfig()
		defaults.Max = &v
		return defaults
	}
	if v, ok := config.(float64); ok {
		maxVal := int(v)
		defaults := DefaultNoMultipleEmptyLinesConfig()
		defaults.Max = &maxVal
		return defaults
	}
	return configutil.Coerce(config, DefaultNoMultipleEmptyLinesConfig())
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewNoMultipleEmptyLinesRule())
}
