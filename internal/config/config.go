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
	Rules RulesConfig `json:"rules" koanf:"rules"`

	// Output configures output format and destination.
	Output OutputConfig `json:"output" koanf:"output"`

	// InlineDirectives controls inline suppression directives.
	InlineDirectives InlineDirectivesConfig `json:"inline-directives" koanf:"inline-directives"`

	// AI configures opt-in AI features (e.g., AI AutoFix).
	AI AIConfig `json:"ai" koanf:"ai"`

	// FileValidation configures pre-parse file validation checks.
	FileValidation FileValidationConfig `json:"file-validation" koanf:"file-validation"`

	// SlowChecks configures async checks that require network or other slow I/O.
	SlowChecks SlowChecksConfig `json:"slow-checks" koanf:"slow-checks"`

	// ConfigFile is the path to the config file that was loaded (if any).
	// This is metadata, not loaded from config.
	ConfigFile string `json:"-" koanf:"-"`
}

// SlowChecksConfig configures async checks that require potentially slow I/O
// (registry access, network, filesystem).
//
// Example TOML configuration:
//
//	[slow-checks]
//	mode = "auto"
//	fail-fast = true
//	timeout = "20s"
type SlowChecksConfig struct {
	// Mode controls when slow checks run: auto (CI detection), on, off.
	Mode string `json:"mode,omitempty" koanf:"mode"`

	// FailFast skips async checks when fast checks already produce SeverityError violations.
	FailFast bool `json:"fail-fast,omitempty" koanf:"fail-fast"`

	// Timeout is the wall-clock budget for all async checks per invocation.
	Timeout string `json:"timeout,omitempty" koanf:"timeout"`
}

// FileValidationConfig configures pre-parse file validation checks.
//
// Example TOML configuration:
//
//	[file-validation]
//	max-file-size = 102400
type FileValidationConfig struct {
	// MaxFileSize is the maximum file size in bytes (0 = unlimited).
	MaxFileSize int64 `json:"max-file-size,omitempty" koanf:"max-file-size"`
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
	Enabled bool `json:"enabled,omitempty" koanf:"enabled"`

	// WarnUnused reports warnings for directives that don't suppress any violations.
	WarnUnused bool `json:"warn-unused,omitempty" koanf:"warn-unused"`

	// ValidateRules reports warnings for unknown rule codes in directives.
	ValidateRules bool `json:"validate-rules,omitempty" koanf:"validate-rules"`

	// RequireReason reports warnings for directives without a reason= explanation.
	RequireReason bool `json:"require-reason,omitempty" koanf:"require-reason"`
}

// AIConfig configures opt-in AI features.
//
// This is intentionally minimal for MVP. Expand cautiously: AI behavior must remain opt-in.
type AIConfig struct {
	// Enabled toggles all AI features in tally. Disabled by default.
	Enabled bool `json:"enabled,omitempty" koanf:"enabled"`

	// Command is the ACP-capable agent program argv (stdio).
	// Example: ["acp-agent", "--model", "foo"].
	Command []string `json:"command,omitempty" koanf:"command"`

	// Timeout is the per-fix timeout (e.g. "90s"). Parsed with time.ParseDuration at runtime.
	Timeout string `json:"timeout,omitempty" koanf:"timeout"`

	// MaxInputBytes limits how much content tally will send to the agent (guards cost/latency).
	MaxInputBytes int `json:"max-input-bytes,omitempty" koanf:"max-input-bytes"`

	// RedactSecrets redacts obvious secrets before sending content to the agent.
	RedactSecrets bool `json:"redact-secrets,omitempty" koanf:"redact-secrets"`
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
		AI: AIConfig{
			Enabled:       false,
			Timeout:       "90s",
			MaxInputBytes: 256 * 1024,
			RedactSecrets: true,
		},
		FileValidation: FileValidationConfig{
			MaxFileSize: 100 * 1024, // 100 KB
		},
		SlowChecks: SlowChecksConfig{
			Mode:     "auto",
			FailFast: true,
			Timeout:  "20s",
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

	// 4. Validate merged raw config and decode.
	cfg, err := decodeConfig(k.Raw())
	if err != nil {
		return nil, err
	}

	cfg.ConfigFile = configPath
	return cfg, nil
}

// knownHyphenatedKeys maps dot-separated patterns to their hyphenated equivalents.
// Add new entries here when adding rules with hyphenated names.
var knownHyphenatedKeys = map[string]string{
	"max.lines":                    "max-lines",
	"skip.blank.lines":             "skip-blank-lines",
	"skip.comments":                "skip-comments",
	"inline.directives":            "inline-directives",
	"warn.unused":                  "warn-unused",
	"validate.rules":               "validate-rules",
	"require.reason":               "require-reason",
	"show.source":                  "show-source",
	"fail.level":                   "fail-level",
	"max.input.bytes":              "max-input-bytes",
	"redact.secrets":               "redact-secrets",
	"slow.checks":                  "slow-checks",
	"fail.fast":                    "fail-fast",
	"newline.between.instructions": "newline-between-instructions",
	"file.validation":              "file-validation",
	"max.file.size":                "max-file-size",
}

var allowedEnvTopLevelKeys = map[string]struct{}{
	"rules":             {},
	"output":            {},
	"inline-directives": {},
	"ai":                {},
	"slow-checks":       {},
	"file-validation":   {},
	// Compatibility aliases normalized in normalizeOutputAliases.
	"format":      {},
	"path":        {},
	"show-source": {},
	"fail-level":  {},
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

	topLevel := s
	if before, _, ok := strings.Cut(s, "."); ok {
		topLevel = before
	}
	if _, ok := allowedEnvTopLevelKeys[topLevel]; !ok {
		return "", nil
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
