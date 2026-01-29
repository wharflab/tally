// Package trustedbaseimage implements hadolint DL3026.
// This rule ensures that base images are only pulled from trusted registries.
package trustedbaseimage

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/distribution/reference"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
)

// Config is the configuration for the trusted-base-image rule.
type Config struct {
	// TrustedRegistries is the list of allowed registries.
	TrustedRegistries []string `json:"trusted-registries,omitempty" jsonschema:"description=Allowed registries. If empty rule is disabled."`
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

// Schema returns the JSON Schema for this rule's configuration.
// Follows ESLint's meta.schema pattern for rule options validation.
func (r *Rule) Schema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"trusted-registries": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "minLength": 1},
				"minItems":    1,
				"uniqueItems": true,
				"description": "Allowed registries (at least one required to enable rule)",
			},
		},
		"additionalProperties": false,
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

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *Rule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

// resolveConfig extracts the Config from input, falling back to defaults.
func (r *Rule) resolveConfig(config any) Config {
	switch v := config.(type) {
	case Config:
		return v
	case *Config:
		if v != nil {
			return *v
		}
	case map[string]any:
		return configutil.Resolve(v, DefaultConfig())
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
