package linter

import (
	"slices"
	"testing"

	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/rules"
)

func TestEnabledRuleCodesPowerShellDefaultEnabled(t *testing.T) {
	t.Parallel()

	enabled := EnabledRuleCodes(config.Default())
	if !slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, want powershell engine enabled by default", enabled)
	}
}

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

func TestEnabledRuleCodesPowerShellDynamicOptionsOffKeepsDefaultEngine(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Powershell = map[string]config.RuleConfig{
		"PSAvoidUsingWriteHost": {
			Severity: "off",
			Options:  map[string]any{"Enable": true},
		},
	}

	enabled := EnabledRuleCodes(cfg)
	if !slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, want default powershell engine despite disabled concrete rule options", enabled)
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

func TestEnabledRuleCodesPowerShellInternalErrorConfigEnablesEngine(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Powershell = map[string]config.RuleConfig{
		"PowerShellInternalError": {
			Severity: "warning",
		},
	}

	enabled := EnabledRuleCodes(cfg)
	if !slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, want powershell engine enabled by internal-error config", enabled)
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

func TestEnabledRuleCodesPowerShellInternalErrorIncludeEnablesEngine(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Include = []string{rules.PowerShellRulePrefix + "PowerShellInternalError"}

	enabled := EnabledRuleCodes(cfg)
	if !slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, want powershell engine enabled by internal-error include", enabled)
	}
}

func TestEnabledRuleCodesPowerShellEngineCanBeDisabled(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Rules.Powershell = map[string]config.RuleConfig{
		"PowerShell": {
			Severity: "off",
		},
	}

	enabled := EnabledRuleCodes(cfg)
	if slices.Contains(enabled, rules.PowerShellRulePrefix+"PowerShell") {
		t.Fatalf("EnabledRuleCodes() = %#v, did not want powershell engine enabled", enabled)
	}
}
