package hadolint

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/configutil"
)

// DL3026Config is the configuration for the trusted-base-image rule.
type DL3026Config struct {
	// TrustedRegistries is the list of allowed registries.
	TrustedRegistries []string `json:"trusted-registries,omitempty" koanf:"trusted-registries"`
}

// DefaultDL3026Config returns the default configuration.
// By default, no trusted registries are configured, so the rule is disabled.
func DefaultDL3026Config() DL3026Config {
	return DL3026Config{
		TrustedRegistries: nil,
	}
}

// DL3026Rule implements the DL3026 linting rule.
type DL3026Rule struct{}

// NewDL3026Rule creates a new DL3026 rule instance.
func NewDL3026Rule() *DL3026Rule {
	return &DL3026Rule{}
}

// Metadata returns the rule metadata.
func (r *DL3026Rule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            rules.HadolintRulePrefix + "DL3026",
		Name:            "Use only trusted base images",
		Description:     "Use only an allowed registry in the FROM image",
		DocURL:          "https://github.com/hadolint/hadolint/wiki/DL3026",
		DefaultSeverity: rules.SeverityOff, // Off by default, enabled when trusted-registries configured
		Category:        "security",
		IsExperimental:  false,
	}
}

// Schema returns the JSON Schema for this rule's configuration.
// Follows ESLint's meta.schema pattern for rule options validation.
func (r *DL3026Rule) Schema() map[string]any {
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object",
		"properties": map[string]any{
			"trusted-registries": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "minLength": 1},
				"uniqueItems": true,
				"description": "Allowed registries (empty or omitted disables the rule)",
			},
		},
		"additionalProperties": false,
	}
}

// Check runs the DL3026 rule.
func (r *DL3026Rule) Check(input rules.LintInput) []rules.Violation {
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
		ref := parseImageRef(stage.BaseName)
		if ref == nil {
			// Can't parse - skip (BuildKit would have caught parse errors)
			continue
		}

		registry := ref.Domain()

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
// Supports wildcard patterns:
//   - "*" matches any registry
//   - "*.example.com" matches any subdomain of example.com (suffix match)
//   - "prefix*" matches any registry starting with prefix (prefix match)
func isRegistryTrusted(registry string, trusted []string) bool {
	normalizedRegistry := normalizeRegistry(registry)

	for _, t := range trusted {
		if matchRegistry(normalizeRegistry(t), normalizedRegistry) {
			return true
		}
	}
	return false
}

// matchRegistry checks if a registry matches a pattern.
// Supports exact match, "*" (any), "*.suffix" (suffix match), and "prefix*" (prefix match).
func matchRegistry(pattern, registry string) bool {
	// Exact wildcard matches everything
	if pattern == "*" {
		return true
	}

	// Suffix wildcard: *.example.com matches foo.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // Keep the dot: ".example.com"
		return strings.HasSuffix(registry, suffix)
	}

	// Prefix wildcard: prefix* matches prefix.anything
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(registry, prefix)
	}

	// Exact match
	return pattern == registry
}

// normalizeRegistry normalizes registry names for comparison.
// Converts Docker Hub aliases to canonical "docker.io".
// Trims whitespace to handle accidental spaces in config files.
func normalizeRegistry(registry string) string {
	registry = strings.ToLower(strings.TrimSpace(registry))
	switch registry {
	case "index.docker.io", "registry-1.docker.io", "registry.hub.docker.com", "hub.docker.com":
		return "docker.io"
	}
	return registry
}

// DefaultConfig returns the default configuration for this rule.
func (r *DL3026Rule) DefaultConfig() any {
	return DefaultDL3026Config()
}

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *DL3026Rule) ValidateConfig(config any) error {
	return configutil.ValidateWithSchema(config, r.Schema())
}

// resolveConfig extracts the DL3026Config from input, falling back to defaults.
func (r *DL3026Rule) resolveConfig(config any) DL3026Config {
	return configutil.Coerce(config, DefaultDL3026Config())
}

// init registers the rule with the default registry.
func init() {
	rules.Register(NewDL3026Rule())
}
