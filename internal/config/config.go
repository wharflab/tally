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
	"github.com/knadh/koanf/providers/env"
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
	Rules RulesConfig `koanf:"rules"`

	// Output configures output format and destination.
	Output OutputConfig `koanf:"output"`

	// InlineDirectives controls inline suppression directives.
	InlineDirectives InlineDirectivesConfig `koanf:"inline-directives"`

	// ConfigFile is the path to the config file that was loaded (if any).
	// This is metadata, not loaded from config.
	ConfigFile string `koanf:"-"`
}

// OutputConfig configures output formatting and behavior.
type OutputConfig struct {
	// Format specifies the output format: "text", "json", "sarif", "github-actions".
	// Default: "text"
	Format string `koanf:"format"`

	// Path specifies where to write output: "stdout", "stderr", or a file path.
	// Default: "stdout"
	Path string `koanf:"path"`

	// ShowSource enables source code snippets in text output.
	// Default: true
	ShowSource bool `koanf:"show-source"`

	// FailLevel sets the minimum severity level that causes a non-zero exit code.
	// Valid values: "error", "warning", "info", "style", "none"
	// Default: "style" (any violation causes exit code 1)
	FailLevel string `koanf:"fail-level"`
}

// RulesConfig contains configuration for all linting rules.
type RulesConfig struct {
	MaxLines MaxLinesRule `koanf:"max-lines"`
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
	// Default: true
	Enabled bool `koanf:"enabled"`

	// WarnUnused reports warnings for directives that don't suppress any violations.
	// Default: false
	WarnUnused bool `koanf:"warn-unused"`

	// ValidateRules reports warnings for unknown rule codes in directives.
	// Default: false (allows BuildKit/hadolint rule codes for migration compatibility)
	ValidateRules bool `koanf:"validate-rules"`

	// RequireReason reports warnings for directives without a reason= explanation.
	// Only applies to tally and hadolint directives (buildx doesn't support reason=).
	// Default: false
	RequireReason bool `koanf:"require-reason"`
}

// MaxLinesRule configures the max-lines rule.
// This rule checks if a Dockerfile exceeds a maximum line count.
//
// Default: 50 lines (excluding blanks and comments).
// This was determined by analyzing 500 public Dockerfiles on GitHub:
// P90 = 53 lines. With skip-blank-lines and skip-comments enabled by default,
// this provides a comfortable margin while flagging unusually long Dockerfiles.
//
// Example TOML configuration:
//
//	[rules.max-lines]
//	max = 50
//	skip-blank-lines = true
//	skip-comments = true
type MaxLinesRule struct {
	// Max is the maximum number of lines allowed. 0 means disabled.
	// Default: 50 (P90 of 500 analyzed Dockerfiles, counting only code lines).
	Max int `koanf:"max"`

	// SkipBlankLines excludes blank lines from the count when true.
	// Default: true (count only meaningful lines).
	SkipBlankLines bool `koanf:"skip-blank-lines"`

	// SkipComments excludes comment lines from the count when true.
	// Default: true (count only instruction lines).
	SkipComments bool `koanf:"skip-comments"`
}

// Enabled returns true if the max-lines rule is enabled.
func (r MaxLinesRule) Enabled() bool {
	return r.Max > 0
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		Output: OutputConfig{
			Format:     "text",
			Path:       "stdout",
			ShowSource: true,
			FailLevel:  "style", // Any violation causes exit code 1
		},
		Rules: RulesConfig{
			MaxLines: MaxLinesRule{
				Max:            50,   // P90 of 500 analyzed Dockerfiles
				SkipBlankLines: true, // Count only meaningful lines
				SkipComments:   true, // Count only instruction lines
			},
		},
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
	if err := k.Load(env.Provider(EnvPrefix, ".", envKeyTransform), nil); err != nil {
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
func envKeyTransform(s string) string {
	// Remove TALLY_ prefix
	s = strings.TrimPrefix(s, EnvPrefix)
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
	return s
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
