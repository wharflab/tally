package tally

import (
	"fmt"
	"strings"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
)

// PreferCurlConfigRuleCode is the full rule code for the prefer-curl-config rule.
const PreferCurlConfigRuleCode = rules.TallyRulePrefix + "prefer-curl-config"

// Default curl config values.
const (
	defaultRetry          = 5
	defaultConnectTimeout = 15
	defaultMaxTime        = 300
)

// Windows-specific constants.
const (
	curlHomeLinux   = "/etc/curl"
	curlHomeWindows = `c:\curl`
	curlExeName     = "curl.exe"
)

// PreferCurlConfigConfig is the optional configuration for the rule.
type PreferCurlConfigConfig struct {
	Retry          *int `json:"retry,omitempty"           koanf:"retry"`
	ConnectTimeout *int `json:"connect-timeout,omitempty" koanf:"connect-timeout"`
	MaxTime        *int `json:"max-time,omitempty"        koanf:"max-time"`
}

// DefaultPreferCurlConfigConfig returns the default configuration.
func DefaultPreferCurlConfigConfig() PreferCurlConfigConfig {
	r, ct, mt := defaultRetry, defaultConnectTimeout, defaultMaxTime
	return PreferCurlConfigConfig{Retry: &r, ConnectTimeout: &ct, MaxTime: &mt}
}

// PreferCurlConfigRule detects stages that use curl and suggests inserting a
// COPY heredoc with retry configuration to make builds more robust against
// transient download failures.
type PreferCurlConfigRule struct {
	schema map[string]any
}

// NewPreferCurlConfigRule creates a new rule instance.
func NewPreferCurlConfigRule() *PreferCurlConfigRule {
	schema, err := configutil.RuleSchema(PreferCurlConfigRuleCode)
	if err != nil {
		panic(err)
	}
	return &PreferCurlConfigRule{schema: schema}
}

// Metadata returns the rule metadata.
func (r *PreferCurlConfigRule) Metadata() rules.RuleMetadata {
	return rules.RuleMetadata{
		Code:            PreferCurlConfigRuleCode,
		Name:            "Prefer curl retry config",
		Description:     "Stages using curl should include a retry config to handle transient failures",
		DocURL:          rules.TallyDocURL(PreferCurlConfigRuleCode),
		DefaultSeverity: rules.SeverityInfo,
		Category:        "reliability",
		FixPriority:     93, //nolint:mnd // After cache-mounts (90), before add-unpack (95)
	}
}

// Schema returns the JSON Schema for this rule's configuration.
func (r *PreferCurlConfigRule) Schema() map[string]any { return r.schema }

// DefaultConfig returns the default configuration.
func (r *PreferCurlConfigRule) DefaultConfig() any { return DefaultPreferCurlConfigConfig() }

// ValidateConfig validates the configuration against the rule's JSON Schema.
func (r *PreferCurlConfigRule) ValidateConfig(config any) error {
	return configutil.ValidateRuleOptions(PreferCurlConfigRuleCode, config)
}

// Check runs the prefer-curl-config rule.
func (r *PreferCurlConfigRule) Check(input rules.LintInput) []rules.Violation {
	cfg := r.resolveConfig(input.Config)
	meta := r.Metadata()
	return checkDownloadConfigStages(input, makeDownloadConfigStageContextBuilder(input, meta, downloadConfigRuleSpec{
		stageEnvKey:      "CURL_HOME",
		linuxEnvValue:    curlHomeLinux,
		windowsEnvValue:  curlHomeWindows,
		destination:      "${CURL_HOME}/.curlrc",
		comment:          "# [tally] curl configuration for improved robustness",
		violationMessage: "stage uses curl without a retry config; consider adding a .curlrc with retry settings",
		violationDetail: "Transient download failures are common during image builds. " +
			"A .curlrc file with --retry settings makes builds more robust. " +
			"The fix inserts a comment, ENV CURL_HOME, and a COPY heredoc with retry defaults.",
		fixDescription:                "Add curl retry config via COPY heredoc",
		content:                       buildCurlConfigContent(cfg),
		hasConfig:                     hasCurlConfig,
		triggerKind:                   curlTriggerKind,
		skipAddUnpackOwnedInvocations: true,
	}))
}

// curlTriggerKind returns the trigger type for a RUN instruction.
// Install takes precedence: a RUN that both installs and invokes curl
// (e.g. `apt-get install -y curl && curl ...`) is classified as install.
func curlTriggerKind(runFacts *facts.RunFacts, isWindows bool) downloadConfigTrigger {
	for i := range runFacts.InstallCommands {
		for j := range runFacts.InstallCommands[i].Packages {
			if runFacts.InstallCommands[i].Packages[j].Normalized == nonPOSIXDownloadCommandCurl {
				return downloadConfigTriggerInstall
			}
		}
	}
	for i := range runFacts.CommandInfos {
		name := runFacts.CommandInfos[i].Name
		if name == nonPOSIXDownloadCommandCurl || (isWindows && name == curlExeName) {
			return downloadConfigTriggerInvocation
		}
	}
	return downloadConfigTriggerNone
}

// hasCurlConfig returns true if any observable file in the stage has a path
// ending in .curlrc or _curlrc (the Windows default name).
func hasCurlConfig(stageFacts *facts.StageFacts) bool {
	for _, f := range stageFacts.ObservableFiles {
		if strings.HasSuffix(f.Path, "/.curlrc") || strings.HasSuffix(f.Path, `\.curlrc`) ||
			strings.HasSuffix(f.Path, "/_curlrc") || strings.HasSuffix(f.Path, `\_curlrc`) {
			return true
		}
	}
	return false
}

// buildCurlConfigContent builds the .curlrc file content from config values.
func buildCurlConfigContent(cfg PreferCurlConfigConfig) string {
	retry := defaultRetry
	if cfg.Retry != nil {
		retry = *cfg.Retry
	}
	connectTimeout := defaultConnectTimeout
	if cfg.ConnectTimeout != nil {
		connectTimeout = *cfg.ConnectTimeout
	}
	maxTime := defaultMaxTime
	if cfg.MaxTime != nil {
		maxTime = *cfg.MaxTime
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--retry-connrefused\n")
	fmt.Fprintf(&sb, "--connect-timeout %d\n", connectTimeout)
	fmt.Fprintf(&sb, "--retry %d\n", retry)
	fmt.Fprintf(&sb, "--max-time %d\n", maxTime)
	return sb.String()
}

func (r *PreferCurlConfigRule) resolveConfig(config any) PreferCurlConfigConfig {
	return configutil.Coerce(config, DefaultPreferCurlConfigConfig())
}

func init() {
	rules.Register(NewPreferCurlConfigRule())
}
