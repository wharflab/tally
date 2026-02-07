package linter

import (
	"sort"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/buildkit"
	"github.com/tinovyatkin/tally/internal/semantic"
)

type heredocRuleOptions struct {
	MinCommands *int `koanf:"min-commands"`
}

// EnabledRuleCodes returns the set of rule codes that are active for the given config.
// Includes registered rules, BuildKit captured rules, and semantic construction rules.
func EnabledRuleCodes(cfg *config.Config) []string {
	enabledSet := make(map[string]struct{})

	// Collect registered rules (tally/*, hadolint/*, and implemented buildkit/* rules).
	registry := rules.DefaultRegistry()
	for _, rule := range registry.All() {
		if isRuleEnabled(rule.Metadata().Code, rule.Metadata().DefaultSeverity, cfg) {
			enabledSet[rule.Metadata().Code] = struct{}{}
		}
	}

	// Collect BuildKit parse-time rules that can be captured by tally.
	for _, info := range buildkit.Captured() {
		ruleCode := rules.BuildKitRulePrefix + info.Name
		if isRuleEnabled(ruleCode, info.DefaultSeverity, cfg) {
			enabledSet[ruleCode] = struct{}{}
		}
	}

	// Collect semantic construction rules (emitted outside the registry).
	for _, ruleCode := range semantic.ConstructionRuleCodes() {
		if isRuleEnabled(ruleCode, rules.SeverityError, cfg) {
			enabledSet[ruleCode] = struct{}{}
		}
	}

	enabled := make([]string, 0, len(enabledSet))
	for ruleCode := range enabledSet {
		enabled = append(enabled, ruleCode)
	}
	sort.Strings(enabled)
	return enabled
}

// isRuleEnabled checks if a rule is effectively enabled based on config.
func isRuleEnabled(ruleCode string, defaultSeverity rules.Severity, cfg *config.Config) bool {
	if cfg == nil {
		return defaultSeverity != rules.SeverityOff
	}

	// Check if explicitly disabled by exclude pattern.
	enabled := cfg.Rules.IsEnabled(ruleCode)
	if enabled != nil {
		return *enabled
	}

	// Respect explicit severity overrides (on/off).
	if sev := cfg.Rules.GetSeverity(ruleCode); sev != "" {
		return sev != "off"
	}

	// Check if "off" rule is auto-enabled by having config options.
	if defaultSeverity == rules.SeverityOff {
		ruleConfig := cfg.Rules.Get(ruleCode)
		return ruleConfig != nil && len(ruleConfig.Options) > 0
	}

	// Use default severity.
	return defaultSeverity != rules.SeverityOff
}

// heredocMinCommands extracts the min-commands setting from the prefer-run-heredoc config.
// Returns 0 if not configured.
func heredocMinCommands(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	opts := config.DecodeRuleOptions(&cfg.Rules, rules.HeredocRuleCode, heredocRuleOptions{})
	if opts.MinCommands == nil {
		return 0
	}
	return *opts.MinCommands
}
