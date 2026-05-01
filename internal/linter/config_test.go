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

func TestEnabledRuleCodesPowerShellDynamicOptionsConfigEnablesEngine(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Powershell = map[string]config.RuleConfig{
		"PSAvoidUsingWriteHost": {
			Options: map[string]any{"Enable": true},
		},
	}

	enabled := EnabledRuleCodes(cfg)
	if !slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, want powershell engine enabled by concrete rule options", enabled)
	}
}

func TestEnabledRuleCodesPowerShellParserDiagnosticConfigEnablesEngine(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Powershell = map[string]config.RuleConfig{
		"InvalidLeftHandSide": {
			Severity: "warning",
		},
	}

	enabled := EnabledRuleCodes(cfg)
	if !slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, want powershell engine enabled by parser diagnostic config", enabled)
	}
}

func TestEnabledRuleCodesPowerShellDynamicIncludeEnablesEngine(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Include = []string{rules.PowerShellRulePrefix + "InvalidLeftHandSide"}

	enabled := EnabledRuleCodes(cfg)
	if !slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, want powershell engine enabled by dynamic include", enabled)
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
