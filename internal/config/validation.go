package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"

	"github.com/wharflab/tally/internal/ruleconfig"
	schemavalidator "github.com/wharflab/tally/internal/schemas/runtime"
)

func decodeConfig(raw map[string]any) (*Config, error) {
	if err := validateAndNormalize(raw); err != nil {
		return nil, err
	}

	normalized := koanf.New(".")
	if err := normalized.Load(confmap.Provider(raw, ""), nil); err != nil {
		return nil, fmt.Errorf("load normalized config: %w", err)
	}

	cfg := &Config{}
	if err := normalized.Unmarshal("", cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func validateAndNormalize(raw map[string]any) error {
	normalizeCompatibilityAliases(raw)
	normalizeRuleShorthand(raw)

	validator, err := schemavalidator.DefaultValidator()
	if err != nil {
		return err
	}
	if err := validator.CoerceRootConfig(raw); err != nil {
		return err
	}
	if err := validator.ValidateRootConfig(raw); err != nil {
		return err
	}
	if err := validateRuleOptions(raw, validator); err != nil {
		return err
	}
	return nil
}

func validateRuleOptions(raw map[string]any, validator schemavalidator.Validator) error {
	rulesRaw, ok := raw["rules"].(map[string]any)
	if !ok {
		return nil
	}

	namespaces := []string{"tally", "hadolint", "buildkit"}
	for _, ns := range namespaces {
		namespaceRaw, ok := rulesRaw[ns].(map[string]any)
		if !ok {
			continue
		}

		for name, entry := range namespaceRaw {
			ruleCode := ns + "/" + name
			if !validator.HasRuleSchema(ruleCode) {
				opts := optionsFromRuleEntry(entry)
				if len(opts) == 0 {
					continue
				}
				optKeys := make([]string, 0, len(opts))
				for key := range opts {
					optKeys = append(optKeys, key)
				}
				slices.Sort(optKeys)
				return fmt.Errorf(
					"rule %s does not support options (%s)",
					ruleCode,
					strings.Join(optKeys, ", "),
				)
			}

			if opts := optionsFromRuleEntry(entry); len(opts) > 0 {
				if err := validator.ValidateRuleOptions(ruleCode, opts); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func normalizeCompatibilityAliases(raw map[string]any) {
	normalizeOutputAliases(raw)
	normalizeLegacyTallyRules(raw)
}

func normalizeOutputAliases(raw map[string]any) {
	outputRaw, ok := raw["output"].(map[string]any)
	if !ok {
		outputRaw = nil
	}
	if outputRaw == nil {
		outputRaw = make(map[string]any)
		raw["output"] = outputRaw
	}

	aliases := map[string]string{
		"format":      "format",
		"path":        "path",
		"show-source": "show-source",
		"fail-level":  "fail-level",
	}
	for from, to := range aliases {
		value, ok := raw[from]
		if !ok {
			continue
		}
		if _, exists := outputRaw[to]; !exists {
			outputRaw[to] = value
		}
		delete(raw, from)
	}
}

func normalizeLegacyTallyRules(raw map[string]any) {
	rulesRaw, ok := raw["rules"].(map[string]any)
	if !ok {
		return
	}

	tallyRaw, ok := rulesRaw["tally"].(map[string]any)
	if !ok {
		tallyRaw = nil
	}
	if tallyRaw == nil {
		tallyRaw = make(map[string]any)
		rulesRaw["tally"] = tallyRaw
	}

	reserved := map[string]struct{}{
		"include":  {},
		"exclude":  {},
		"tally":    {},
		"hadolint": {},
		"buildkit": {},
	}

	for key, value := range rulesRaw {
		if _, isReserved := reserved[key]; isReserved {
			continue
		}
		if _, exists := tallyRaw[key]; !exists {
			tallyRaw[key] = value
		}
		delete(rulesRaw, key)
	}
}

func optionsFromRuleEntry(entry any) map[string]any {
	obj, ok := entry.(map[string]any)
	if !ok {
		return nil
	}
	if len(obj) == 0 {
		return nil
	}

	options := make(map[string]any, len(obj))
	maps.Copy(options, obj)
	delete(options, "severity")
	delete(options, "fix")
	delete(options, "exclude")
	if len(options) == 0 {
		return nil
	}
	return options
}

func normalizeRuleShorthand(raw map[string]any) {
	rulesRaw, ok := raw["rules"].(map[string]any)
	if !ok {
		return
	}

	ruleconfig.CanonicalizeRulesMap(rulesRaw)
}
