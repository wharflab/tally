package tally

import (
	"fmt"
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
)

// PreferWgetConfigRuleCode is the full rule code for the prefer-wget-config rule.
const PreferWgetConfigRuleCode = rules.TallyRulePrefix + "prefer-wget-config"

const (
	defaultWgetTimeout = 15
	defaultWgetTries   = 5
)

const (
	wgetRCLinux   = "/etc/wgetrc"
	wgetRCWindows = `c:\wgetrc`
	wgetExeName   = "wget.exe"
)

// PreferWgetConfigConfig is the optional configuration for the rule.
type PreferWgetConfigConfig struct {
	Timeout *int `json:"timeout,omitempty" koanf:"timeout"`
	Tries   *int `json:"tries,omitempty"   koanf:"tries"`
}

// DefaultPreferWgetConfigConfig returns the default configuration.
func DefaultPreferWgetConfigConfig() PreferWgetConfigConfig {
	timeout, tries := defaultWgetTimeout, defaultWgetTries
	return PreferWgetConfigConfig{Timeout: &timeout, Tries: &tries}
}

// PreferWgetConfigRule detects stages that use wget and suggests inserting a
// COPY heredoc with retry configuration to make builds more robust against
// transient download failures.
type PreferWgetConfigRule struct {
	schema map[string]any
}

// NewPreferWgetConfigRule creates a new rule instance.
func NewPreferWgetConfigRule() *PreferWgetConfigRule {
	schema, err := configutil.RuleSchema(PreferWgetConfigRuleCode)
	if err != nil {
		panic(err)
	}
	return &PreferWgetConfigRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *PreferWgetConfigRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferWgetConfigRuleCode,
		Name:            "Prefer wget retry config",
		Description:     "Stages using wget should include a retry config to handle transient failures",
		DocURL:          rules.TallyDocURL(PreferWgetConfigRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "reliability",
		FixPriority:     94, //nolint:mnd // After curl config (93), before add-unpack (95)
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferWgetConfigRule) Schema() map[string]any { return r.schema }

// DefaultConfig returns the default configuration.
func (r *PreferWgetConfigRule) DefaultConfig() any { return DefaultPreferWgetConfigConfig() }

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferWgetConfigRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(PreferWgetConfigRuleCode, config)
}

// Check runs the prefer-wget-config rule.
func (r *PreferWgetConfigRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	meta := r.Metadata()
	return checkDownloadConfigStages(input, makeDownloadConfigStageContextBuilder(input, meta, downloadConfigRuleSpec{
		stageEnvKey:      "WGETRC",
		linuxEnvValue:    wgetRCLinux,
		windowsEnvValue:  wgetRCWindows,
		destination:      "${WGETRC}",
		comment:          "# [tally] wget configuration for improved robustness",
		violationMessage: "stage uses wget without a retry config; consider adding wgetrc with retry settings",
		violationDetail: "Transient download failures are common during image builds. " +
			"A wgetrc file with retry settings makes builds more robust. " +
			"The fix inserts a comment, ENV WGETRC, and a COPY heredoc with retry defaults.",
		fixDescription:                "Add wget retry config via COPY heredoc",
		content:                       buildWgetConfigContent(cfg),
		hasConfig:                     hasWgetConfig,
		triggerKind:                   wgetTriggerKind,
		skipAddUnpackOwnedInvocations: true,
	}))
}

func wgetTriggerKind(runFacts *facts.RunFacts, isWindows bool) downloadConfigTrigger {
	for i := range runFacts.InstallCommands {
		for j := range runFacts.InstallCommands[i].Packages {
			if runFacts.InstallCommands[i].Packages[j].Normalized == nonPOSIXDownloadCommandWget {
				return downloadConfigTriggerInstall
			}
		}
	}
	for i := range runFacts.CommandInfos {
		name := runFacts.CommandInfos[i].Name
		if name == nonPOSIXDownloadCommandWget || (isWindows && name == wgetExeName) {
			return downloadConfigTriggerInvocation
		}
	}
	return downloadConfigTriggerNone
}

func hasWgetConfig(stageFacts *facts.StageFacts) bool {
	return stageFacts.HasObservablePathSuffix("/wgetrc", "/.wgetrc")
}

func buildWgetConfigContent(cfg PreferWgetConfigConfig) string {
	timeout := defaultWgetTimeout
	if cfg.Timeout != nil {
		timeout = *cfg.Timeout
	}
	tries := defaultWgetTries
	if cfg.Tries != nil {
		tries = *cfg.Tries
	}

	var sb strings.Builder
	sb.WriteString("retry_connrefused = on\n")
	fmt.Fprintf(&sb, "timeout = %d\n", timeout)
	fmt.Fprintf(&sb, "tries = %d\n", tries)
	return sb.String()
}

func (r *PreferWgetConfigRule) resolveConfig(config any) PreferWgetConfigConfig {
	return configutil.Coerce(config, DefaultPreferWgetConfigConfig())
}

func init() {
	rules.Register(NewPreferWgetConfigRule())
}
