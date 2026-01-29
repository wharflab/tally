package config

import (
	"strings"
)

// RuleConfig represents per-rule configuration.
// Can be specified in TOML as:
//
//	[rules.tally.max-lines]
//	severity = "warning"
//	# Rule-specific options are flattened at this level
//	max = 100
//	skip-blank-lines = true
type RuleConfig struct {
	// Severity overrides the rule's default severity.
	Severity string `json:"severity,omitempty" jsonschema:"enum=error,enum=warning,enum=info,enum=style" koanf:"severity"`

	// Exclude contains path patterns where this rule should not run.
	Exclude ExcludeConfig `json:"exclude" koanf:"exclude"`

	// Options contains rule-specific configuration options.
	Options map[string]any `json:"-" koanf:",remain"`
}

// ExcludeConfig defines file exclusion patterns for a rule.
type ExcludeConfig struct {
	// Paths contains glob patterns for files to exclude.
	Paths []string `json:"paths,omitempty" jsonschema:"description=Glob patterns for files to exclude (e.g. test/**)" koanf:"paths"`
}

// RulesConfig contains rule selection and per-rule configuration.
//
// Example TOML (Ruff-style selection):
//
//	[rules]
//	include = ["buildkit/*"]                    # Enable all buildkit rules
//	exclude = ["buildkit/MaintainerDeprecated"] # Disable specific rules
//
//	[rules.tally.max-lines]
//	severity = "warning"
//	max = 100
//
//	[rules.hadolint.DL3026]
//	severity = "warning"
//	trusted-registries = ["docker.io", "gcr.io"]
type RulesConfig struct {
	// Include explicitly enables rules.
	Include []string `json:"include,omitempty" jsonschema:"description=Enable rules by pattern (e.g. buildkit/*)" koanf:"include"`

	// Exclude explicitly disables rules. Takes precedence over Include.
	Exclude []string `json:"exclude,omitempty" jsonschema:"description=Disable rules by pattern (takes precedence)" koanf:"exclude"`

	// Tally contains configuration for tally/* rules.
	Tally map[string]RuleConfig `json:"tally,omitempty" jsonschema:"description=Configuration for tally/* rules" koanf:"tally"`

	// Buildkit contains configuration for buildkit/* rules.
	Buildkit map[string]RuleConfig `json:"buildkit,omitempty" jsonschema:"description=Configuration for buildkit/* rules" koanf:"buildkit"`

	// Hadolint contains configuration for hadolint/* rules.
	Hadolint map[string]RuleConfig `json:"hadolint,omitempty" jsonschema:"description=Configuration for hadolint/* rules" koanf:"hadolint"`
}

// Get returns the configuration for a specific rule.
// Returns nil if no configuration exists for the rule.
// ruleCode should be namespaced (e.g., "buildkit/StageNameCasing").
func (rc *RulesConfig) Get(ruleCode string) *RuleConfig {
	if rc == nil {
		return nil
	}
	ns, name := parseRuleCode(ruleCode)
	nsMap := rc.namespaceMap(ns)
	if nsMap == nil {
		return nil
	}
	if cfg, ok := nsMap[name]; ok {
		return &cfg
	}
	return nil
}

// parseRuleCode parses a rule code into namespace and name.
// "buildkit/StageNameCasing" -> ("buildkit", "StageNameCasing")
// "max-lines" -> ("", "max-lines")
func parseRuleCode(ruleCode string) (string, string) {
	if idx := strings.Index(ruleCode, "/"); idx > 0 {
		return ruleCode[:idx], ruleCode[idx+1:]
	}
	return "", ruleCode
}

// IsEnabled checks if a rule is enabled based on Include/Exclude patterns.
// Returns nil if no configuration specifies enabled/disabled (use rule default).
// Exclude takes precedence over Include.
func (rc *RulesConfig) IsEnabled(ruleCode string) *bool {
	if rc == nil {
		return nil
	}

	// Check Exclude first (takes precedence)
	if matchesAnyPattern(ruleCode, rc.Exclude) {
		return boolPtr(false)
	}

	// Check Include
	if matchesAnyPattern(ruleCode, rc.Include) {
		return boolPtr(true)
	}

	// No explicit config - use rule default
	return nil
}

// matchesAnyPattern checks if ruleCode matches any pattern in the list.
// Patterns can be:
// - Exact match: "buildkit/StageNameCasing"
// - Namespace wildcard: "buildkit/*"
func matchesAnyPattern(ruleCode string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchesPattern(ruleCode, pattern) {
			return true
		}
	}
	return false
}

// matchesPattern checks if ruleCode matches a single pattern.
func matchesPattern(ruleCode, pattern string) bool {
	// Exact match
	if ruleCode == pattern {
		return true
	}

	// Namespace wildcard: "buildkit/*" matches "buildkit/StageNameCasing"
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		ns, _ := parseRuleCode(ruleCode)
		return ns == prefix
	}

	return false
}

// GetSeverity returns the severity override for a rule.
// Returns empty string if no override is configured.
func (rc *RulesConfig) GetSeverity(ruleCode string) string {
	if rc == nil {
		return ""
	}
	if cfg := rc.Get(ruleCode); cfg != nil && cfg.Severity != "" {
		return cfg.Severity
	}
	return ""
}

// GetExcludePaths returns the exclusion patterns for a rule.
func (rc *RulesConfig) GetExcludePaths(ruleCode string) []string {
	if rc == nil {
		return nil
	}
	if cfg := rc.Get(ruleCode); cfg != nil {
		if cfg.Exclude.Paths == nil {
			return nil
		}
		out := make([]string, len(cfg.Exclude.Paths))
		copy(out, cfg.Exclude.Paths)
		return out
	}
	return nil
}

// GetOptions returns rule-specific options.
// Returns nil if no options are configured.
func (rc *RulesConfig) GetOptions(ruleCode string) map[string]any {
	if rc == nil {
		return nil
	}
	if cfg := rc.Get(ruleCode); cfg != nil {
		return cfg.Options
	}
	return nil
}

// Set stores configuration for a rule.
// Creates the namespace map if nil.
// Returns false if the namespace is unknown.
func (rc *RulesConfig) Set(ruleCode string, cfg RuleConfig) bool {
	ns, name := parseRuleCode(ruleCode)
	switch ns {
	case "tally":
		if rc.Tally == nil {
			rc.Tally = make(map[string]RuleConfig)
		}
		rc.Tally[name] = cfg
		return true
	case "buildkit":
		if rc.Buildkit == nil {
			rc.Buildkit = make(map[string]RuleConfig)
		}
		rc.Buildkit[name] = cfg
		return true
	case "hadolint":
		if rc.Hadolint == nil {
			rc.Hadolint = make(map[string]RuleConfig)
		}
		rc.Hadolint[name] = cfg
		return true
	default:
		return false
	}
}

// namespaceMap returns the map for a given namespace.
func (rc *RulesConfig) namespaceMap(ns string) map[string]RuleConfig {
	switch ns {
	case "tally":
		return rc.Tally
	case "buildkit":
		return rc.Buildkit
	case "hadolint":
		return rc.Hadolint
	default:
		return nil
	}
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// MaxLinesOptions contains options for the tally/max-lines rule.
// Used for typed access to rule options.
type MaxLinesOptions struct {
	// Max is the maximum number of lines allowed. 0 means disabled.
	Max int `koanf:"max"`

	// SkipBlankLines excludes blank lines from the count when true.
	SkipBlankLines bool `koanf:"skip-blank-lines"`

	// SkipComments excludes comment lines from the count when true.
	SkipComments bool `koanf:"skip-comments"`
}

// DefaultMaxLinesOptions returns the default max-lines rule options.
// Default: 50 lines (P90 of 500 analyzed Dockerfiles), excluding blanks and comments.
func DefaultMaxLinesOptions() MaxLinesOptions {
	return MaxLinesOptions{
		Max:            50,   // P90 of 500 analyzed Dockerfiles
		SkipBlankLines: true, // Count only meaningful lines
		SkipComments:   true, // Count only instruction lines
	}
}

// GetMaxLinesOptions extracts MaxLinesOptions from rule config options.
// Returns defaults if not configured.
func GetMaxLinesOptions(rc *RulesConfig) MaxLinesOptions {
	defaults := DefaultMaxLinesOptions()

	if rc == nil {
		return defaults
	}

	opts := rc.GetOptions("tally/max-lines")
	if opts == nil {
		return defaults
	}

	// Extract options with defaults
	switch v := opts["max"].(type) {
	case int:
		defaults.Max = v
	case int64:
		defaults.Max = int(v)
	case float64:
		defaults.Max = int(v)
	}

	if skip, ok := opts["skip-blank-lines"].(bool); ok {
		defaults.SkipBlankLines = skip
	}

	if skip, ok := opts["skip-comments"].(bool); ok {
		defaults.SkipComments = skip
	}

	return defaults
}
