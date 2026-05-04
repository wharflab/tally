package config

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"

	"github.com/wharflab/tally/internal/ruleconfig"
	"github.com/wharflab/tally/internal/ruledeprecation"
	schemasembed "github.com/wharflab/tally/internal/schemas"
	generatedconfig "github.com/wharflab/tally/internal/schemas/generated/config"
	schemavalidator "github.com/wharflab/tally/internal/schemas/runtime"
)

func decodeConfig(raw map[string]any) (*Config, error) {
	if err := validateAndNormalize(raw); err != nil {
		return nil, err
	}

	schemaCfg, err := decodeSchemaConfig(raw)
	if err != nil {
		return nil, err
	}

	rulesCfg, err := decodeRulesConfig(raw)
	if err != nil {
		return nil, err
	}

	cfg := configFromSchema(schemaCfg)
	cfg.Rules = rulesCfg
	return cfg, nil
}

func decodeSchemaConfig(raw map[string]any) (*generatedconfig.TallyConfigSchemaJson, error) {
	data, err := jsonv2.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal normalized config: %w", err)
	}

	var schemaCfg generatedconfig.TallyConfigSchemaJson
	if err := jsonv2.Unmarshal(data, &schemaCfg); err != nil {
		return nil, fmt.Errorf("decode generated config: %w", err)
	}
	return &schemaCfg, nil
}

func decodeRulesConfig(raw map[string]any) (RulesConfig, error) {
	rulesRaw, ok := raw["rules"].(map[string]any)
	if !ok {
		return RulesConfig{}, nil
	}

	normalized := koanf.New(".")
	if err := normalized.Load(confmap.Provider(rulesRaw, ""), nil); err != nil {
		return RulesConfig{}, fmt.Errorf("load normalized rule config: %w", err)
	}

	var rulesCfg RulesConfig
	if err := normalized.Unmarshal("", &rulesCfg); err != nil {
		return RulesConfig{}, fmt.Errorf("decode rule config: %w", err)
	}
	return rulesCfg, nil
}

func configFromSchema(schemaCfg *generatedconfig.TallyConfigSchemaJson) *Config {
	cfg := &Config{}
	if schemaCfg == nil {
		return cfg
	}

	if output := schemaCfg.Output; output != nil {
		cfg.Output = OutputConfig{
			Format:     string(output.Format),
			Path:       output.Path,
			ShowSource: output.ShowSource,
			FailLevel:  string(output.FailLevel),
		}
	}

	if inline := schemaCfg.InlineDirectives; inline != nil {
		cfg.InlineDirectives = InlineDirectivesConfig{
			Enabled:       inline.Enabled,
			WarnUnused:    inline.WarnUnused,
			ValidateRules: inline.ValidateRules,
			RequireReason: inline.RequireReason,
		}
	}

	if ai := schemaCfg.Ai; ai != nil {
		cfg.AI = AIConfig{
			Enabled:       ai.Enabled,
			Command:       slices.Clone(ai.Command),
			Timeout:       ai.Timeout,
			MaxInputBytes: ai.MaxInputBytes,
			RedactSecrets: ai.RedactSecrets,
		}
	}

	if fv := schemaCfg.FileValidation; fv != nil {
		cfg.FileValidation = FileValidationConfig{
			MaxFileSize: int64(fv.MaxFileSize),
		}
	}

	if slowChecks := schemaCfg.SlowChecks; slowChecks != nil {
		cfg.SlowChecks = SlowChecksConfig{
			Mode:     string(slowChecks.Mode),
			FailFast: slowChecks.FailFast,
			Timeout:  slowChecks.Timeout,
		}
	}

	return cfg
}

func validateAndNormalize(raw map[string]any) error {
	normalizeCompatibilityAliases(raw)
	normalizeRuleShorthand(raw)
	if err := normalizeNestedRuleTables(raw); err != nil {
		return err
	}

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

func normalizeNestedRuleTables(raw map[string]any) error {
	rulesRaw, ok := raw["rules"].(map[string]any)
	if !ok {
		return nil
	}

	for namespace, entry := range rulesRaw {
		namespaceRaw, ok := entry.(map[string]any)
		if !ok || namespace == "include" || namespace == "exclude" {
			continue
		}

		normalized := make(map[string]any, len(namespaceRaw))
		for name, ruleEntry := range namespaceRaw {
			if err := flattenNestedRuleTable(normalized, namespace, []string{name}, ruleEntry); err != nil {
				return err
			}
		}
		rulesRaw[namespace] = normalized
	}
	return nil
}

func flattenNestedRuleTable(out map[string]any, namespace string, nameParts []string, entry any) error {
	entryMap, isMap := entry.(map[string]any)
	ruleName := strings.Join(nameParts, "/")
	if !isMap || isRuleConfigEntry(namespace, ruleName, entryMap) || hasRuleOptionShape(entryMap) {
		if _, exists := out[ruleName]; exists {
			return fmt.Errorf("rule %s/%s is configured more than once", namespace, ruleName)
		}
		out[ruleName] = entry
		return nil
	}

	for childName, childEntry := range entryMap {
		childParts := append(slices.Clone(nameParts), childName)
		if err := flattenNestedRuleTable(out, namespace, childParts, childEntry); err != nil {
			return err
		}
	}
	return nil
}

func isRuleConfigEntry(namespace, ruleName string, entry map[string]any) bool {
	if _, ok := schemasembed.RuleSchemaID(namespace + "/" + ruleName); ok {
		return true
	}
	return entryHasAny(entry, "severity", "fix", "exclude")
}

func hasRuleOptionShape(entry map[string]any) bool {
	if len(entry) == 0 {
		return true
	}
	for _, value := range entry {
		if _, ok := value.(map[string]any); !ok {
			return true
		}
	}
	return false
}

func entryHasAny(entry map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := entry[key]; ok {
			return true
		}
	}
	return false
}

func validateRuleOptions(raw map[string]any, validator schemavalidator.Validator) error {
	rulesRaw, ok := raw["rules"].(map[string]any)
	if !ok {
		return nil
	}

	for _, ns := range schemasembed.RuleNamespaces() {
		namespaceRaw, ok := rulesRaw[ns].(map[string]any)
		if !ok {
			continue
		}

		for name, entry := range namespaceRaw {
			ruleCode := ns + "/" + name
			schemaRuleCode := ruleCode
			if replacement, ok := ruledeprecation.ReplacementFor(ruleCode); ok {
				schemaRuleCode = replacement
			}
			if !validator.HasRuleSchema(schemaRuleCode) {
				opts := optionsFromRuleEntry(entry)
				if len(opts) == 0 {
					continue
				}
				optKeys := slices.Sorted(maps.Keys(opts))
				return fmt.Errorf(
					"rule %s does not support options (%s)",
					ruleCode,
					strings.Join(optKeys, ", "),
				)
			}

			if opts := optionsFromRuleEntry(entry); len(opts) > 0 {
				if err := validator.ValidateRuleOptions(schemaRuleCode, opts); err != nil {
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

	outputKeys := []string{"format", "path", "show-source", "fail-level"}
	for _, key := range outputKeys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if _, exists := outputRaw[key]; !exists {
			outputRaw[key] = value
		}
		delete(raw, key)
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
		"include": {},
		"exclude": {},
	}
	for _, ns := range schemasembed.RuleNamespaces() {
		reserved[ns] = struct{}{}
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
