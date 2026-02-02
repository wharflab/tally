// Package config provides configuration loading and discovery for tally.
//
// Configuration is loaded from multiple sources with the following priority
// (highest to lowest):
//  1. CLI flags
//  2. Environment variables (TALLY_* prefix)
//  3. Config file (closest .tally.toml or tally.toml)
//  4. Built-in defaults
//
// Config file discovery follows a cascading pattern similar to Ruff:
// starting from the target file's directory, walk up the filesystem
// until a config file is found. The closest config wins (no merging).
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// ConfigFileNames defines the config file names to search for, in priority order.
var ConfigFileNames = []string{".tally.toml", "tally.toml"}

// EnvPrefix is the prefix for environment variables.
const EnvPrefix = "TALLY_"

// Config represents the complete tally configuration.
type Config struct {
	// Rules contains configuration for individual linting rules.
	Rules RulesConfig `json:"rules" jsonschema:"description=Rule configuration" koanf:"rules"`

	// Output configures output format and destination.
	Output OutputConfig `json:"output" jsonschema:"description=Output settings" koanf:"output"`

	// InlineDirectives controls inline suppression directives.
	InlineDirectives InlineDirectivesConfig `json:"inline-directives" koanf:"inline-directives"`

	// ConfigFile is the path to the config file that was loaded (if any).
	// This is metadata, not loaded from config.
	ConfigFile string `json:"-" koanf:"-"`
}

// OutputConfig configures output formatting and behavior.
type OutputConfig struct {
	// Format specifies the output format.
	Format string `json:"format,omitempty" koanf:"format"`

	// Path specifies where to write output.
	Path string `json:"path,omitempty" koanf:"path"`

	// ShowSource enables source code snippets in text output.
	ShowSource bool `json:"show-source,omitempty" koanf:"show-source"`

	// FailLevel sets the minimum severity level that causes a non-zero exit code.
	FailLevel string `json:"fail-level,omitempty" koanf:"fail-level"`
}

// InlineDirectivesConfig controls inline suppression directives.
// Supports # tally ignore=..., # hadolint ignore=..., and # check=skip=...
//
// Example TOML configuration:
//
//	[inline-directives]
//	enabled = true
//	warn-unused = false
//	validate-rules = true
//	require-reason = false
type InlineDirectivesConfig struct {
	// Enabled controls whether inline directives are processed.
	Enabled bool `json:"enabled,omitempty" jsonschema:"default=true,description=Process inline ignore directives" koanf:"enabled"`

	// WarnUnused reports warnings for directives that don't suppress any violations.
	WarnUnused bool `json:"warn-unused,omitempty" jsonschema:"default=false,description=Warn about unused directives" koanf:"warn-unused"`

	// ValidateRules reports warnings for unknown rule codes in directives.
	ValidateRules bool `json:"validate-rules,omitempty" jsonschema:"default=false" koanf:"validate-rules"`

	// RequireReason reports warnings for directives without a reason= explanation.
	RequireReason bool `json:"require-reason,omitempty" jsonschema:"default=false" koanf:"require-reason"`
}

// Default returns the default configuration.
// Rule-specific defaults are owned by each rule via ConfigurableRule.DefaultConfig().
func Default() *Config {
	return &Config{
		Output: OutputConfig{
			Format:     "text",
			Path:       "stdout",
			ShowSource: true,
			FailLevel:  "style", // Any violation causes exit code 1
		},
		Rules: RulesConfig{}, // Empty - defaults come from rules
		InlineDirectives: InlineDirectivesConfig{
			Enabled:       true,  // Process inline directives by default
			WarnUnused:    false, // Don't warn about unused directives by default
			ValidateRules: false, // Don't validate rule codes (allows BuildKit/hadolint rules)
			RequireReason: false, // Don't require reason= by default
		},
	}
}

// Load loads configuration for a target file path.
// It discovers the closest config file, loads it, and applies
// environment variable overrides.
func Load(targetPath string) (*Config, error) {
	return loadWithConfigPath(Discover(targetPath))
}

// LoadFromFile loads configuration from a specific config file path.
// Unlike Load, it does not perform config discovery.
func LoadFromFile(configPath string) (*Config, error) {
	return loadWithConfigPath(configPath)
}

// loadWithConfigPath is an internal helper that loads config with an optional config file path.
func loadWithConfigPath(configPath string) (*Config, error) {
	k := koanf.New(".")

	// 1. Load defaults
	if err := k.Load(structs.Provider(Default(), "koanf"), nil); err != nil {
		return nil, err
	}

	// 2. Load config file if provided
	if configPath != "" {
		if err := k.Load(file.Provider(configPath), toml.Parser()); err != nil {
			return nil, err
		}
	}

	// 3. Load environment variables (TALLY_* prefix)
	// TALLY_RULES_MAX_LINES_MAX -> rules.max-lines.max
	if err := k.Load(env.Provider(".", env.Opt{
		Prefix:        EnvPrefix,
		TransformFunc: envKeyTransform,
	}), nil); err != nil {
		return nil, err
	}

	// 4. Unmarshal into config struct
	cfg := &Config{}
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, err
	}

	cfg.ConfigFile = configPath
	return cfg, nil
}

// knownHyphenatedKeys maps dot-separated patterns to their hyphenated equivalents.
// Add new entries here when adding rules with hyphenated names.
var knownHyphenatedKeys = map[string]string{
	"max.lines":         "max-lines",
	"skip.blank.lines":  "skip-blank-lines",
	"skip.comments":     "skip-comments",
	"inline.directives": "inline-directives",
	"warn.unused":       "warn-unused",
	"validate.rules":    "validate-rules",
	"require.reason":    "require-reason",
	"show.source":       "show-source",
	"fail.level":        "fail-level",
}

// envKeyTransform converts environment variable names to config keys.
// TALLY_FORMAT -> format
// TALLY_RULES_MAX_LINES_MAX -> rules.max-lines.max
func envKeyTransform(k, v string) (string, any) {
	// Remove TALLY_ prefix (already stripped by Prefix option, but keeping for safety)
	s := strings.TrimPrefix(k, EnvPrefix)
	// Convert to lowercase and replace _ with . for nesting
	s = strings.ToLower(s)
	// Handle the special case of max-lines (underscores in key names)
	// RULES_MAX_LINES_MAX -> rules.max-lines.max
	// RULES_MAX_LINES_SKIP_BLANK_LINES -> rules.max-lines.skip-blank-lines
	s = strings.ReplaceAll(s, "_", ".")
	// Fix known hyphenated keys using lookup table
	for pattern, replacement := range knownHyphenatedKeys {
		s = strings.ReplaceAll(s, pattern, replacement)
	}
	return s, v
}

// Discover finds the closest config file for a target file path.
// It walks up the directory tree from the target's directory,
// checking for config files at each level.
// Returns empty string if no config file is found.
func Discover(targetPath string) string {
	// Get absolute path to handle relative paths correctly
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return ""
	}

	// Start from the target's directory
	dir := filepath.Dir(absPath)

	for {
		// Check each config file name in priority order
		for _, name := range ConfigFileNames {
			configPath := filepath.Join(dir, name)
			if fileExists(configPath) {
				return configPath
			}
		}

		// Move up to parent directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return ""
}

// fileExists checks if a file exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
