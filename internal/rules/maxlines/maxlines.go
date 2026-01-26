// Package maxlines implements the max-lines rule for Dockerfile linting.
package maxlines

import (
	"fmt"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Config is the configuration for the max-lines rule.
type Config struct {
	// Max is the maximum number of lines allowed (0 = disabled).
	Max int

	// SkipBlankLines excludes blank lines from the count.
	SkipBlankLines bool

	// SkipComments excludes comment lines from the count.
	SkipComments bool
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Max:            0, // Disabled by default
		SkipBlankLines: false,
		SkipComments:   false,
	}
}

// Rule implements the max-lines linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:             "max-lines",
		Name:             "Maximum Lines",
		Description:      "Limits the maximum number of lines in a Dockerfile",
		DocURL:           "https://github.com/tinovyatkin/tally/blob/main/docs/rules/max-lines.md",
		DefaultSeverity:  rules.SeverityError,
		Category:         "maintainability",
		EnabledByDefault: false, // Requires configuration
		IsExperimental:   false,
	}
}

// Check runs the max-lines rule.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)

	// Rule is disabled if Max is 0
	if cfg.Max <= 0 {
		return nil
	}

	// Count lines
	count := countLines(input.Lines, cfg)

	if count > cfg.Max {
		return []rules.Violation{
			rules.NewViolation(
				rules.NewFileLocation(input.File),
				r.Metadata().Code,
				fmt.Sprintf("file has %d lines, maximum allowed is %d", count, cfg.Max),
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL),
		}
	}

	return nil
}

// DefaultConfig returns the default configuration for this rule.
func (r *Rule) DefaultConfig() any {
	return DefaultConfig()
}

// ValidateConfig checks if the configuration is valid.
func (r *Rule) ValidateConfig(config any) error {
	if config == nil {
		return nil
	}
	var cfg Config
	switch v := config.(type) {
	case Config:
		cfg = v
	case *Config:
		if v == nil {
			return nil
		}
		cfg = *v
	default:
		return fmt.Errorf("expected Config, got %T", config)
	}
	if cfg.Max < 0 {
		return fmt.Errorf("max must be >= 0, got %d", cfg.Max)
	}
	return nil
}

// resolveConfig extracts the Config from input, falling back to defaults.
func (r *Rule) resolveConfig(config any) Config {
	if config == nil {
		return DefaultConfig()
	}
	if cfg, ok := config.(Config); ok {
		return cfg
	}
	// Try pointer
	if cfg, ok := config.(*Config); ok && cfg != nil {
		return *cfg
	}
	return DefaultConfig()
}

// countLines counts lines according to the configuration.
func countLines(lines []string, cfg Config) int {
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip blank lines if configured
		if cfg.SkipBlankLines && trimmed == "" {
			continue
		}

		// Skip comments if configured
		if cfg.SkipComments && strings.HasPrefix(trimmed, "#") {
			continue
		}

		count++
	}
	return count
}

// New creates a new max-lines rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
