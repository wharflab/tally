package tally

import (
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
)

// EolLastRuleCode is the full rule code for the eol-last rule.
const EolLastRuleCode = rules.TallyRulePrefix + "eol-last"

const (
	eolModeAlways = "always"
	eolModeNever  = "never"
)

// EolLastConfig is the configuration for the eol-last rule.
type EolLastConfig struct {
	// Mode controls whether files must end with a newline ("always", default)
	// or must not ("never").
	Mode *string `json:"mode,omitempty" koanf:"mode"`
}

// DefaultEolLastConfig returns the default configuration.
func DefaultEolLastConfig() EolLastConfig {
	mode := eolModeAlways
	return EolLastConfig{Mode: &mode}
}

// EolLastRule implements the eol-last linting rule.
type EolLastRule struct {
	schema map[string]any
}

// NewEolLastRule creates a new eol-last rule instance.
func NewEolLastRule() *EolLastRule {
	schema, err := configutil.RuleSchema(EolLastRuleCode)
	if err != nil {
		panic(err)
	}
	return &EolLastRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *EolLastRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            EolLastRuleCode,
		Name:            "EOL Last",
		Description:     "Enforces a newline at the end of non-empty files",
		DocURL:          rules.TallyDocURL(EolLastRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		FixPriority:     99,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *EolLastRule) Schema() map[string]any {
	return r.schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *EolLastRule) DefaultConfig() any {
	return DefaultEolLastConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *EolLastRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(EolLastRuleCode, config)
}

// Check runs the eol-last rule.
func (r *EolLastRule) Check(input rules.LintInput) []rules.Violation {
	source := input.Source
	if len(source) == 0 {
		return nil
	}

	cfg := r.resolveConfig(input.Config)
	meta := r.Metadata()
	mode := eolModeAlways
	if cfg.Mode != nil {
		mode = *cfg.Mode
	}

	switch mode {
	case eolModeAlways:
		return r.checkAlways(input, source, meta)
	case eolModeNever:
		return r.checkNever(input, source, meta)
	default:
		return nil
	}
}

// checkAlways reports a violation when the file does not end with a newline.
func (r *EolLastRule) checkAlways(input rules.LintInput, source []byte, meta rules.RuleMetadata) []rules.Violation {
	if source[len(source)-1] == '\n' {
		return nil
	}

	sm := input.SourceMap()
	lines := sm.Lines()
	lastLine := len(lines) // 1-based
	lastCol := len(lines[lastLine-1])

	// Zero-width insertion at end of file.
	loc := rules.NewRangeLocation(input.File, lastLine, lastCol, lastLine, lastCol)

	v := rules.NewViolation(loc, meta.Code, "file must end with a newline", meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Add final newline",
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits: []rules.TextEdit{
				{Location: loc, NewText: "\n"},
			},
			IsPreferred: true,
		})

	return []rules.Violation{v}
}

// checkNever reports a violation when the file ends with a newline.
func (r *EolLastRule) checkNever(input rules.LintInput, source []byte, meta rules.RuleMetadata) []rules.Violation {
	if source[len(source)-1] != '\n' {
		return nil
	}

	sm := input.SourceMap()
	lines := sm.Lines()
	effCount := effectiveLineCount(lines, source)
	if effCount == 0 {
		return nil
	}

	// Count trailing newlines: from the last content line through all blank
	// lines plus the line terminator that effectiveLineCount drops.
	// We find the first non-blank line scanning backwards from the end.
	lastContentIdx := effCount - 1 // 0-based
	for lastContentIdx > 0 && lines[lastContentIdx] == "" {
		lastContentIdx--
	}

	// Emit one edit per trailing newline line. Each edit removes exactly one
	// line boundary (\n). Separate edits allow the fixer to skip individual
	// edits that overlap with no-multiple-empty-lines without discarding the
	// entire fix.
	var edits []rules.TextEdit

	// First edit: the \n at the end of the last content line.
	contentLine := lastContentIdx + 1 // 1-based
	edits = append(edits, rules.TextEdit{
		Location: rules.NewRangeLocation(input.File, contentLine, len(lines[lastContentIdx]), contentLine+1, 0),
		NewText:  "",
	})

	// Subsequent edits: one per trailing blank line.
	for i := lastContentIdx + 1; i < effCount; i++ {
		lineNum := i + 1 // 1-based
		edits = append(edits, rules.TextEdit{
			Location: rules.NewRangeLocation(input.File, lineNum, 0, lineNum+1, 0),
			NewText:  "",
		})
	}

	// Report violation at the last content line (where the unwanted \n begins).
	loc := rules.NewRangeLocation(input.File, contentLine, len(lines[lastContentIdx]), contentLine, len(lines[lastContentIdx]))

	v := rules.NewViolation(loc, meta.Code, "file must not end with a newline", meta.DefaultSeverity).
		WithDocURL(meta.DocURL).
		WithSuggestedFix(&rules.SuggestedFix{
			Description: "Remove trailing newline(s)",
			Safety:      rules.FixSafe,
			Priority:    meta.FixPriority,
			Edits:       edits,
			IsPreferred: true,
		})

	return []rules.Violation{v}
}

// resolveConfig extracts the config from input, falling back to defaults.
func (r *EolLastRule) resolveConfig(config any) EolLastConfig {
	return configutil.Coerce(config, DefaultEolLastConfig())
}

func init() {
	rules.Register(NewEolLastRule())
}
