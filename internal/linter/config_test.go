package linter

import (
	"slices"
	"testing"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/rules"
)

func TestEnabledRuleCodesPowerShellDynamicConfigEnablesEngine(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Powershell = map[string]config.RuleConfig{
		"PSAvoidUsingWriteHost": {
			Severity: "warning",
		},
	}

	enabled := EnabledRuleCodes(cfg)
	if !slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, want powershell engine enabled by concrete rule config", enabled)
	}
}

func TestEnabledRuleCodesPowerShellDynamicConfigOffDoesNotEnableEngine(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Powershell = map[string]config.RuleConfig{
		"PSAvoidUsingWriteHost": {
			Severity: "off",
		},
	}

	enabled := EnabledRuleCodes(cfg)
	if slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, did not want powershell engine enabled", enabled)
	}
}
