package powershell

import (
	"maps"
	"sort"
	"strings"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/rules"
)

func analyzerSettings(cfg any) psanalyzer.Settings {
	rulesCfg, ok := cfg.(*config.RulesConfig)
	if !ok || rulesCfg == nil {
		return psanalyzer.Settings{}
	}

	var settings psanalyzer.Settings
	excludeSet := make(map[string]struct{})

	addPowerShellRulePatterns(excludeSet, rulesCfg, rulesCfg.Exclude)

	for name, ruleCfg := range rulesCfg.Powershell {
		if !isAnalyzerRuleName(name) {
			continue
		}
		if _, excluded := excludeSet[name]; excluded {
			continue
		}
		if ruleCfg.Severity == config.SeverityOffValue {
			excludeSet[name] = struct{}{}
			continue
		}
		if len(ruleCfg.Options) == 0 {
			continue
		}
		if settings.Rules == nil {
			settings.Rules = make(map[string]map[string]any)
		}
		options := make(map[string]any, len(ruleCfg.Options))
		maps.Copy(options, ruleCfg.Options)
		settings.Rules[name] = options
	}

	settings.ExcludeRules = sortedRuleNames(excludeSet)
	return settings
}

func addPowerShellRulePatterns(out map[string]struct{}, rulesCfg *config.RulesConfig, patterns []string) {
	for _, pattern := range patterns {
		name, ok := strings.CutPrefix(pattern, rules.PowerShellRulePrefix)
		if !ok || !isAnalyzerRuleName(name) {
			continue
		}
		if enabled := rulesCfg.IsEnabled(pattern); enabled != nil && *enabled {
			continue
		}
		out[name] = struct{}{}
	}
}

func sortedRuleNames(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isAnalyzerRuleName(name string) bool {
	switch name {
	case "", "PowerShell", "PowerShellInternalError":
		return false
	}
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
