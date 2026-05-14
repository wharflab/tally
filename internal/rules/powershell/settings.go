package powershell

import (
	"maps"
	"sort"
	"strings"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/psanalyzer"
	"github.com/wharflab/tally/internal/rules"
)

// defaultExcludedAnalyzerRules lists PSScriptAnalyzer rule names that tally
// disables by default for Dockerfile RUN snippets. The exclusion can be
// overridden by including the rule explicitly (`include = ["powershell/<Name>"]`)
// or by setting a non-off severity / options entry under
// `rules.powershell.<Name>` in `.tally.toml`.
//
// Rules land here when they target script reusability, function/module
// authoring, manifest authoring, DSC resources, or pure style — concerns
// that don't apply to a one-shot RUN. Rules that catch real correctness,
// security, or compatibility issues are left enabled.
var defaultExcludedAnalyzerRules = map[string]struct{}{
	// Reusability concerns — RUN has no downstream pipeline.
	"PSAvoidUsingWriteHost":            {},
	"PSAvoidUsingPositionalParameters": {},

	// Function / cmdlet authoring — a RUN executes statements, not exported APIs.
	"PSProvideCommentHelp":                        {},
	"PSUseShouldProcessForStateChangingFunctions": {},
	"PSUseSupportsShouldProcess":                  {},
	"PSShouldProcess":                             {},
	"PSAvoidShouldContinueWithoutForce":           {},
	"PSUseSingularNouns":                          {},
	"PSUseApprovedVerbs":                          {},
	"PSReservedCmdletChar":                        {},
	"PSReservedParams":                            {},
	"PSAvoidNullOrEmptyHelpMessageAttribute":      {},
	"PSAvoidDefaultValueForMandatoryParameter":    {},
	"PSAvoidDefaultValueSwitchParameter":          {},
	"PSAvoidGlobalAliases":                        {},
	"PSAvoidGlobalFunctions":                      {},
	"PSAvoidGlobalVars":                           {},
	"PSReviewUnusedParameter":                     {},
	"PSUseOutputTypeCorrectly":                    {},
	"PSUseProcessBlockForPipelineCommand":         {},

	// Module manifest rules — Dockerfiles don't ship `.psd1` files.
	"PSMissingModuleManifestField":         {},
	"PSUseToExportFieldsInManifest":        {},
	"PSAvoidUsingDeprecatedManifestFields": {},

	// Desired State Configuration — out of scope for container builds.
	"PSDSCDscExamplesPresent":                    {},
	"PSDSCDscTestsPresent":                       {},
	"PSDSCReturnCorrectTypesForDSCFunctions":     {},
	"PSDSCStandardDSCFunctionsInResource":        {},
	"PSDSCUseIdenticalMandatoryParametersForDSC": {},
	"PSDSCUseIdenticalParametersForDSC":          {},
	"PSDSCUseVerboseMessageInDSCResource":        {},

	// File-encoding rules — RUN bodies aren't files on disk.
	"PSUseBOMForUnicodeEncodedFile": {},
	"PSUseUTF8EncodingForHelpFile":  {},
}

func analyzerSettings(cfg any) psanalyzer.Settings {
	rulesCfg, ok := cfg.(*config.RulesConfig)
	if !ok || rulesCfg == nil {
		return defaultAnalyzerSettings()
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

	addDefaultExcludedRules(excludeSet, rulesCfg)

	settings.ExcludeRules = sortedRuleNames(excludeSet)
	return settings
}

// defaultAnalyzerSettings returns the analyzer settings to use when no rules
// config is available. It still applies the default-exclude list so the
// curated set of off-by-default PSSA rules stays consistent.
func defaultAnalyzerSettings() psanalyzer.Settings {
	excludeSet := make(map[string]struct{}, len(defaultExcludedAnalyzerRules))
	for name := range defaultExcludedAnalyzerRules {
		excludeSet[name] = struct{}{}
	}
	return psanalyzer.Settings{ExcludeRules: sortedRuleNames(excludeSet)}
}

// addDefaultExcludedRules merges tally's curated default-disabled PSSA rules
// into out, skipping any rule the user has explicitly opted into.
func addDefaultExcludedRules(out map[string]struct{}, rulesCfg *config.RulesConfig) {
	for name := range defaultExcludedAnalyzerRules {
		if _, already := out[name]; already {
			continue
		}
		if userOptedIn(rulesCfg, name) {
			continue
		}
		out[name] = struct{}{}
	}
}

// userOptedIn reports whether the user has explicitly re-enabled a
// default-disabled PSSA rule. Re-enable signals:
//   - an Include pattern naming the rule directly, the powershell/* namespace,
//     or the universal *. The engine-only selector "powershell/PowerShell"
//     does NOT count — it toggles the engine, not individual default-off rules.
//   - a `rules.powershell.<Name>` entry with non-off severity, or with options
//     (matching the existing per-rule behavior in this file).
func userOptedIn(rulesCfg *config.RulesConfig, name string) bool {
	if rulesCfg == nil {
		return false
	}
	code := rules.PowerShellRulePrefix + name
	for _, pattern := range rulesCfg.Include {
		switch pattern {
		case "*", rules.PowerShellRulePrefix + "*", code:
			return true
		}
	}
	cfg, ok := rulesCfg.Powershell[name]
	if !ok {
		return false
	}
	if cfg.Severity == config.SeverityOffValue {
		return false
	}
	return cfg.Severity != "" || len(cfg.Options) > 0
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
