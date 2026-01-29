// Package trustedbaseimage implements hadolint DL3026.
// This rule ensures that base images are only pulled from trusted registries.
package trustedbaseimage

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/distribution/reference"

	"github.com/tinovyatkin/tally/internal/rules"
)

// Config is the configuration for the trusted-base-image rule.
type Config struct {
	// TrustedRegistries is the list of allowed registries.
	// Images must come from one of these registries.
	// Example: ["docker.io", "gcr.io", "my-registry.com"]
	// If empty, the rule is disabled (all registries allowed).
	TrustedRegistries []string
}

// DefaultConfig returns the default configuration.
// By default, no trusted registries are configured, so the rule is disabled.
func DefaultConfig() Config {
	return Config{
		TrustedRegistries: nil,
	}
}

// Rule implements the DL3026 linting rule.
type Rule struct{}

// Metadata returns the rule metadata.
func (r *Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:             rules.HadolintRulePrefix + "DL3026",
		Name:             "Use only trusted base images",
		Description:      "Use only an allowed registry in the FROM image",
		DocURL:           "https://github.com/hadolint/hadolint/wiki/DL3026",
		DefaultSeverity:  rules.SeverityError,
		Category:         "security",
		EnabledByDefault: false, // Disabled by default since it requires configuration
		IsExperimental:   false,
	}
}

// Check runs the DL3026 rule.
func (r *Rule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)

	// If no trusted registries configured, rule is disabled
	if len(cfg.TrustedRegistries) == 0 {
		return nil
	}

	// Build a set of stage names for quick lookup
	stageNames := make(map[string]bool)
	for i, stage := range input.Stages {
		if stage.Name != "" {
			stageNames[strings.ToLower(stage.Name)] = true
		}
		// Numeric index is also valid
		stageNames[strconv.Itoa(i)] = true
	}

	var violations []rules.Violation

	for _, stage := range input.Stages {
		// Skip scratch - it's a special "no base" image
		if stage.BaseName == "scratch" {
			continue
		}

		// Skip stage references (FROM stagename)
		if stageNames[strings.ToLower(stage.BaseName)] {
			continue
		}

		// Parse the image reference
		named, err := reference.ParseNormalizedNamed(stage.BaseName)
		if err != nil {
			// Can't parse - skip (BuildKit would have caught parse errors)
			continue
		}

		registry := reference.Domain(named)

		if !isRegistryTrusted(registry, cfg.TrustedRegistries) {
			loc := rules.NewLocationFromRanges(input.File, stage.Location)
			violations = append(violations, rules.NewViolation(
				loc,
				r.Metadata().Code,
				fmt.Sprintf(
					"image %q is from untrusted registry %q (allowed: %s)",
					stage.BaseName,
					registry,
					strings.Join(cfg.TrustedRegistries, ", "),
				),
				r.Metadata().DefaultSeverity,
			).WithDocURL(r.Metadata().DocURL))
		}
	}

	return violations
}

// isRegistryTrusted checks if a registry is in the trusted list.
// Handles docker.io aliases (docker.io, index.docker.io, registry-1.docker.io).
func isRegistryTrusted(registry string, trusted []string) bool {
	normalizedRegistry := normalizeRegistry(registry)

	for _, t := range trusted {
		if normalizeRegistry(t) == normalizedRegistry {
			return true
		}
	}
	return false
}

// normalizeRegistry normalizes registry names for comparison.
// Converts Docker Hub aliases to canonical "docker.io".
func normalizeRegistry(registry string) string {
	registry = strings.ToLower(registry)
	switch registry {
	case "index.docker.io", "registry-1.docker.io", "registry.hub.docker.com":
		return "docker.io"
	}
	return registry
}

// DefaultConfig returns the default configuration for this rule.
func (r *Rule) DefaultConfig() any {
	return DefaultConfig()
}

// ValidateConfig checks if the configuration is valid.
func (r *Rule) ValidateConfig(config any) error {
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
	if cfg, ok := config.(*Config); ok && cfg != nil {
		return *cfg
	}
	// Try map[string]any (from config system)
	if opts, ok := config.(map[string]any); ok {
		cfg := DefaultConfig()
		if registries, ok := opts["trusted-registries"].([]any); ok {
			for _, r := range registries {
				if s, ok := r.(string); ok {
					cfg.TrustedRegistries = append(cfg.TrustedRegistries, s)
				}
			}
		}
		// Also try string slice directly
		if registries, ok := opts["trusted-registries"].([]string); ok {
			cfg.TrustedRegistries = registries
		}
		return cfg
	}
	return DefaultConfig()
}

// New creates a new DL3026 rule instance.
func New() *Rule {
	return &Rule{}
}

// init registers the rule with the default registry.
func init() {
	rules.Register(New())
}
