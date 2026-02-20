package tally

import (
	"strings"

	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
)

// NoTrailingSpacesRuleCode is the full rule code for the no-trailing-spaces rule.
const NoTrailingSpacesRuleCode = rules.TallyRulePrefix + "no-trailing-spaces"

// NoTrailingSpacesConfig is the configuration for the no-trailing-spaces rule.
type NoTrailingSpacesConfig struct {
	// SkipBlankLines skips lines that consist entirely of whitespace.
	SkipBlankLines *bool `json:"skip-blank-lines,omitempty" koanf:"skip-blank-lines"`

	// IgnoreComments skips any line whose first non-whitespace character is #.
	// This includes Dockerfile comments and # lines inside heredoc bodies.
	IgnoreComments *bool `json:"ignore-comments,omitempty" koanf:"ignore-comments"`
}

// DefaultNoTrailingSpacesConfig returns the default configuration.
func DefaultNoTrailingSpacesConfig() NoTrailingSpacesConfig {
	skipBlankLines := false
	ignoreComments := false
	return NoTrailingSpacesConfig{
		SkipBlankLines: &skipBlankLines,
		IgnoreComments: &ignoreComments,
	}
}

// NoTrailingSpacesRule implements the no-trailing-spaces linting rule.
type NoTrailingSpacesRule struct{}

// NewNoTrailingSpacesRule creates a new no-trailing-spaces rule instance.
func NewNoTrailingSpacesRule() *NoTrailingSpacesRule {
	return &NoTrailingSpacesRule{}
}

// Metadata returns the rule metadata.
func (r *NoTrailingSpacesRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            NoTrailingSpacesRuleCode,
		Name:            "No Trailing Spaces",
		Description:     "Disallows trailing whitespace at the end of lines",
		DocURL:          rules.TallyDocURL(NoTrailingSpacesRuleCode),
		DefaultSeverity: rules.SeverityStyle,
		Category:        "style",
		IsExperimental:  false,
		FixPriority:     10,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *NoTrailingSpacesRule) Schema() map[string]any {
	schema, err := configutil.RuleSchema(NoTrailingSpacesRuleCode)
	if err != nil {
		panic(err)
	}
	return schema
}

// DefaultConfig returns the default configuration for this rule.
func (r *NoTrailingSpacesRule) DefaultConfig() any {
	return DefaultNoTrailingSpacesConfig()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *NoTrailingSpacesRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(NoTrailingSpacesRuleCode, config)
}

// Check runs the no-trailing-spaces rule.
func (r *NoTrailingSpacesRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	meta := r.Metadata()
	sm := input.SourceMap()

	skipBlankLines := cfg.SkipBlankLines != nil && *cfg.SkipBlankLines
	ignoreComments := cfg.IgnoreComments != nil && *cfg.IgnoreComments

	var violations []rules.Violation

	for i, line := range sm.Lines() {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == line {
			continue // no trailing whitespace
		}

		// Skip lines that are entirely whitespace.
		if skipBlankLines && trimmed == "" {
			continue
		}

		// Skip any line starting with # (Dockerfile comments and # lines in heredocs).
		if ignoreComments && strings.HasPrefix(strings.TrimLeft(trimmed, " \t"), "#") {
			continue
		}

		lineNum := i + 1 // SourceMap is 0-based, locations are 1-based
		loc := rules.NewRangeLocation(input.File, lineNum, len(trimmed), lineNum, len(line))
		v := rules.NewViolation(loc, meta.Code, "trailing whitespace", meta.DefaultSeverity).
			WithDocURL(meta.DocURL).
			WithSuggestedFix(&rules.SuggestedFix{
				Description: "Remove trailing whitespace",
				Safety:      rules.FixSafe,
				Priority:    meta.FixPriority,
				Edits: []rules.TextEdit{
					{
						Location: loc,
						NewText:  "",
					},
				},
				IsPreferred: true,
			})
		violations = append(violations, v)
	}

	return violations
}

// resolveConfig extracts the config from input, falling back to defaults.
func (r *NoTrailingSpacesRule) resolveConfig(config any) NoTrailingSpacesConfig {
	return configutil.Coerce(config, DefaultNoTrailingSpacesConfig())
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewNoTrailingSpacesRule())
}
