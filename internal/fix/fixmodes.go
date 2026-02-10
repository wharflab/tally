package fix

import "github.com/tinovyatkin/tally/internal/config"

// BuildFixModes extracts per-rule fix mode settings from a config.
// Returned keys use the canonical rule code format: "<namespace>/<ruleName>".
//
// Nil is returned when cfg is nil.
func BuildFixModes(cfg *config.Config) map[string]FixMode {
	if cfg == nil {
		return nil
	}

	modes := make(map[string]FixMode)

	addFromNamespace := func(namespace string, ruleConfigs map[string]config.RuleConfig) {
		for name, ruleCfg := range ruleConfigs {
			if ruleCfg.Fix == "" {
				continue
			}
			ruleCode := namespace + "/" + name
			modes[ruleCode] = ruleCfg.Fix
		}
	}

	if cfg.Rules.Tally != nil {
		addFromNamespace("tally", cfg.Rules.Tally)
	}
	if cfg.Rules.Buildkit != nil {
		addFromNamespace("buildkit", cfg.Rules.Buildkit)
	}
	if cfg.Rules.Hadolint != nil {
		addFromNamespace("hadolint", cfg.Rules.Hadolint)
	}

	return modes
}
