package config

import (
	"strings"
)

// RuleConfig represents per-rule configuration.
// Can be specified in TOML as:
//
//	[rules."tally/max-lines"]
//	enabled = true
//	severity = "warning"
//	# Rule-specific options are flattened at this level
//	max = 100
//	skip-blank-lines = true
type RuleConfig struct {
	// Enabled controls whether the rule runs.
	// nil means use the rule's default (EnabledByDefault from metadata).
	Enabled *bool `koanf:"enabled"`

	// Severity overrides the rule's default severity.
	// Empty string means use the rule's default.
	// Valid values: "error", "warning", "info", "style"
	Severity string `koanf:"severity"`

	// Exclude contains path patterns where this rule should not run.
	Exclude ExcludeConfig `koanf:"exclude"`

	// Options contains rule-specific configuration options.
	// For max-lines: max, skip-blank-lines, skip-comments
	// These are stored as a map and passed to the rule's Config.
	Options map[string]any `koanf:",remain"`
}

// ExcludeConfig defines file exclusion patterns for a rule.
type ExcludeConfig struct {
	// Paths contains glob patterns for files to exclude.
	// Example: ["test/**", "testdata/**", "*_test.go"]
	Paths []string `koanf:"paths"`
}

// RulesConfig contains per-rule configuration as a map.
// Keys use namespaced format: "buildkit/StageNameCasing", "tally/max-lines", etc.
//
// Example TOML:
//
//	[rules."tally/max-lines"]
//	enabled = true
//	severity = "warning"
//	max = 100
//
//	[rules."buildkit/MaintainerDeprecated"]
//	enabled = false
//
//	[rules.defaults]
//	"buildkit/*" = { enabled = true }
type RulesConfig struct {
	// PerRule maps rule code to its configuration.
	PerRule map[string]RuleConfig `koanf:",remain"`

	// Defaults contains namespace-level defaults.
	// Keys are patterns like "buildkit/*", "tally/*", "hadolint/*".
	Defaults map[string]RuleConfig `koanf:"defaults"`
}

// Get returns the configuration for a specific rule.
// Returns nil if no configuration exists for the rule.
func (rc *RulesConfig) Get(ruleCode string) *RuleConfig {
	if rc == nil || rc.PerRule == nil {
		return nil
	}
	if cfg, ok := rc.PerRule[ruleCode]; ok {
		return &cfg
	}
	return nil
}

// IsEnabled checks if a rule is enabled.
// Priority: explicit rule config > namespace default > rule's EnabledByDefault.
// Returns nil if no configuration specifies enabled/disabled (use rule default).
func (rc *RulesConfig) IsEnabled(ruleCode string) *bool {
	if rc == nil {
		return nil
	}

	// Check explicit rule config first
	if cfg := rc.Get(ruleCode); cfg != nil && cfg.Enabled != nil {
		return cfg.Enabled
	}

	// Check namespace defaults
	if rc.Defaults != nil {
		ns := getNamespace(ruleCode)
		pattern := ns + "/*"
		if def, ok := rc.Defaults[pattern]; ok && def.Enabled != nil {
			return def.Enabled
		}
	}

	// No config - return nil to indicate "use rule default"
	return nil
}

// GetSeverity returns the severity override for a rule.
// Returns empty string if no override is configured.
func (rc *RulesConfig) GetSeverity(ruleCode string) string {
	if rc == nil {
		return ""
	}

	// Check explicit rule config first
	if cfg := rc.Get(ruleCode); cfg != nil && cfg.Severity != "" {
		return cfg.Severity
	}

	// Check namespace defaults
	if rc.Defaults != nil {
		ns := getNamespace(ruleCode)
		pattern := ns + "/*"
		if def, ok := rc.Defaults[pattern]; ok && def.Severity != "" {
			return def.Severity
		}
	}

	return ""
}

// GetExcludePaths returns the exclusion patterns for a rule.
// Combines rule-specific and namespace-level exclusions.
func (rc *RulesConfig) GetExcludePaths(ruleCode string) []string {
	if rc == nil {
		return nil
	}

	var paths []string

	// Add namespace defaults first
	if rc.Defaults != nil {
		ns := getNamespace(ruleCode)
		pattern := ns + "/*"
		if def, ok := rc.Defaults[pattern]; ok {
			paths = append(paths, def.Exclude.Paths...)
		}
	}

	// Add rule-specific exclusions (may override namespace patterns)
	if cfg := rc.Get(ruleCode); cfg != nil {
		paths = append(paths, cfg.Exclude.Paths...)
	}

	return paths
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
// Creates the PerRule map if nil.
func (rc *RulesConfig) Set(ruleCode string, cfg RuleConfig) {
	if rc.PerRule == nil {
		rc.PerRule = make(map[string]RuleConfig)
	}
	rc.PerRule[ruleCode] = cfg
}

// getNamespace extracts the namespace from a rule code.
// "buildkit/StageNameCasing" -> "buildkit"
// "max-lines" -> "" (no namespace)
func getNamespace(ruleCode string) string {
	if idx := strings.Index(ruleCode, "/"); idx > 0 {
		return ruleCode[:idx]
	}
	return ""
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
