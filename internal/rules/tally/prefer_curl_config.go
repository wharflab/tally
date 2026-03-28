package tally

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"

	"github.com/wharflab/tally/internal/facts"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/rules/configutil"
	"github.com/wharflab/tally/internal/semantic"
	"github.com/wharflab/tally/internal/shell"
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

// curlCheckContext bundles per-stage parameters for the check methods.
type curlCheckContext struct {
	file     string
	curlHome string

	isWindows bool
	meta      rules.RuleMetadata
	cfg       PreferCurlConfigConfig
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
		Category:        "correctness",
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

	fileFacts, _ := input.Facts.(*facts.FileFacts) //nolint:errcheck // nil-safe assertion
	sem, _ := input.Semantic.(*semantic.Model)     //nolint:errcheck // nil fallback

	// Tracks stages that will have curl config after fixes are applied.
	// A child stage (FROM parentStage) is suppressed when the parent
	// is in this set — the fix on the parent propagates via inheritance.
	configuredStages := map[int]bool{}

	var violations []rules.Violation

	for stageIdx, stage := range input.Stages {
		isWindows := stageIsWindows(stageIdx, sem, fileFacts)

		ctx := curlCheckContext{
			file:      input.File,
			curlHome:  curlHomeLinux,
			isWindows: isWindows,
			meta:      meta,
			cfg:       cfg,
		}
		if isWindows {
			ctx.curlHome = curlHomeWindows
		}

		// Suppress if this stage inherits from a stage that already has
		// (or will have after fix) the curl config.
		if parentConfigured(stageIdx, sem, configuredStages) {
			configuredStages[stageIdx] = true
			continue
		}

		v := r.checkStage(stageIdx, stage, fileFacts, sem, &ctx)
		if v != nil {
			// Stage will get the config via fix.
			configuredStages[stageIdx] = true
			violations = append(violations, *v)
		} else if stageHasCurlConfig(stageIdx, fileFacts) {
			// Stage already has the config — propagate to children.
			configuredStages[stageIdx] = true
		}
	}

	return violations
}

// parentConfigured returns true if this stage's base image is a local stage
// reference that already has (or will have) the curl config.
func parentConfigured(stageIdx int, sem *semantic.Model, configured map[int]bool) bool {
	if sem == nil {
		return false
	}
	info := sem.StageInfo(stageIdx)
	if info == nil || info.BaseImage == nil || !info.BaseImage.IsStageRef {
		return false
	}
	return configured[info.BaseImage.StageIndex]
}

// stageHasCurlConfig returns true if the stage already has CURL_HOME set or
// a .curlrc observable — meaning child stages will inherit the config.
func stageHasCurlConfig(stageIdx int, fileFacts *facts.FileFacts) bool {
	if fileFacts == nil {
		return false
	}
	sf := fileFacts.Stage(stageIdx)
	if sf == nil {
		return false
	}
	if sf.EffectiveEnv.Values["CURL_HOME"] != "" {
		return true
	}
	return hasCurlConfig(sf)
}

func stageIsWindows(stageIdx int, sem *semantic.Model, ff *facts.FileFacts) bool {
	if sem != nil {
		if info := sem.StageInfo(stageIdx); info != nil && info.BaseImageOS == semantic.BaseImageOSWindows {
			return true
		}
	}
	if ff != nil {
		if sf := ff.Stage(stageIdx); sf != nil && sf.BaseImageOS == semantic.BaseImageOSWindows {
			return true
		}
	}
	return false
}

func (r *PreferCurlConfigRule) checkStage(
	stageIdx int,
	stage instructions.Stage,
	fileFacts *facts.FileFacts,
	sem *semantic.Model,
	ctx *curlCheckContext,
) *rules.Violation {
	// Facts-first path.
	if fileFacts != nil {
		if stageFacts := fileFacts.Stage(stageIdx); stageFacts != nil {
			return r.checkStageWithFacts(stageFacts, stage, ctx)
		}
	}

	// Fallback: direct stage iteration without facts.
	shellVariant := shell.VariantBash
	if sem != nil {
		if info := sem.StageInfo(stageIdx); info != nil {
			shellVariant = info.ShellSetting.Variant
		}
	}
	return r.checkStageDirect(stage, shellVariant, ctx)
}

func (r *PreferCurlConfigRule) checkStageWithFacts(
	stageFacts *facts.StageFacts,
	stage instructions.Stage,
	ctx *curlCheckContext,
) *rules.Violation {
	// Suppress if CURL_HOME is already set.
	if stageFacts.EffectiveEnv.Values["CURL_HOME"] != "" {
		return nil
	}

	// Suppress if any observable file looks like a curl config.
	// FileContent does exact path lookup; we also scan ObservableFiles
	// for any .curlrc at an arbitrary path (e.g. /root/.curlrc,
	// custom CURL_HOME from a parent stage).
	if hasCurlConfig(stageFacts) {
		return nil
	}

	// Find the first RUN that uses or installs curl.
	for _, runFacts := range stageFacts.Runs {
		if runFacts == nil || !runFacts.UsesShell {
			continue
		}

		switch curlTriggerKind(runFacts, ctx.isWindows) {
		case curlTriggerNone:
			continue

		case curlTriggerInstall:
			return r.buildViolation(runFacts.Run, runFacts.Run, ctx)

		case curlTriggerInvocation:
			firstRun := firstRunInStage(stage)
			if firstRun == nil {
				firstRun = runFacts.Run
			}
			return r.buildViolation(runFacts.Run, firstRun, ctx)
		}
	}

	return nil
}

// curlTrigger classifies how a RUN relates to curl.
type curlTrigger int

const (
	curlTriggerNone       curlTrigger = iota
	curlTriggerInstall                // curl is being installed as a package
	curlTriggerInvocation             // curl is invoked directly
)

// curlTriggerKind returns the trigger type for a RUN instruction.
// Install takes precedence: a RUN that both installs and invokes curl
// (e.g. `apt-get install -y curl && curl ...`) is classified as install.
func curlTriggerKind(runFacts *facts.RunFacts, isWindows bool) curlTrigger {
	for i := range runFacts.InstallCommands {
		for j := range runFacts.InstallCommands[i].Packages {
			if runFacts.InstallCommands[i].Packages[j].Normalized == nonPOSIXDownloadCommandCurl {
				return curlTriggerInstall
			}
		}
	}
	for i := range runFacts.CommandInfos {
		name := runFacts.CommandInfos[i].Name
		if name == nonPOSIXDownloadCommandCurl || (isWindows && name == curlExeName) {
			return curlTriggerInvocation
		}
	}
	return curlTriggerNone
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

// firstRunInStage returns the first RunCommand in a stage, or nil.
func firstRunInStage(stage instructions.Stage) *instructions.RunCommand {
	for _, cmd := range stage.Commands {
		if run, ok := cmd.(*instructions.RunCommand); ok {
			return run
		}
	}
	return nil
}

func (r *PreferCurlConfigRule) checkStageDirect(
	stage instructions.Stage,
	shellVariant shell.Variant,
	ctx *curlCheckContext,
) *rules.Violation {
	cmdNames := []string{nonPOSIXDownloadCommandCurl}
	if ctx.isWindows {
		cmdNames = append(cmdNames, curlExeName)
	}

	for _, cmd := range stage.Commands {
		run, ok := cmd.(*instructions.RunCommand)
		if !ok || !run.PrependShell {
			continue
		}

		script := getRunScriptFromCmd(run)
		if script == "" {
			continue
		}

		// Check for curl package install — insert before this RUN.
		for _, ic := range shell.FindInstallPackages(script, shellVariant) {
			for _, pkg := range ic.Packages {
				if pkg.Normalized == nonPOSIXDownloadCommandCurl {
					return r.buildViolation(run, run, ctx)
				}
			}
		}

		// Check for direct curl invocation — insert before first RUN.
		if cmds := shell.FindCommands(script, shellVariant, cmdNames...); len(cmds) > 0 {
			firstRun := firstRunInStage(stage)
			if firstRun == nil {
				firstRun = run
			}
			return r.buildViolation(run, firstRun, ctx)
		}
	}

	return nil
}

// buildViolation creates a violation anchored at violationRun with a fix
// that inserts before insertBeforeRun.
func (r *PreferCurlConfigRule) buildViolation(
	violationRun, insertBeforeRun *instructions.RunCommand,
	ctx *curlCheckContext,
) *rules.Violation {
	loc := rules.NewLocationFromRanges(ctx.file, violationRun.Location())
	v := rules.NewViolation(
		loc,
		ctx.meta.Code,
		"stage uses curl without a retry config; consider adding a .curlrc with retry settings",
		ctx.meta.DefaultSeverity,
	).WithDocURL(ctx.meta.DocURL).WithDetail(
		"Transient download failures are common during image builds. " +
			"A .curlrc file with --retry settings makes builds more robust. " +
			"The fix inserts ENV CURL_HOME and a COPY heredoc with retry defaults.",
	)

	if fix := r.buildFix(insertBeforeRun, ctx); fix != nil {
		v = v.WithSuggestedFix(fix)
	}

	return &v
}

func (r *PreferCurlConfigRule) buildFix(
	insertBeforeRun *instructions.RunCommand,
	ctx *curlCheckContext,
) *rules.SuggestedFix {
	runLoc := insertBeforeRun.Location()
	if len(runLoc) == 0 {
		return nil
	}

	insertLine := runLoc[0].Start.Line
	insertCol := runLoc[0].Start.Character

	content := buildCurlConfigContent(ctx.cfg)
	copyHeredoc := buildCurlCopyHeredoc(content, ctx.isWindows)
	newText := fmt.Sprintf("ENV CURL_HOME=%s\n%s\n", ctx.curlHome, copyHeredoc)

	return &rules.SuggestedFix{
		Description: "Add curl retry config via COPY heredoc",
		Safety:      rules.FixSuggestion,
		Priority:    ctx.meta.FixPriority,
		Edits: []rules.TextEdit{{
			Location: rules.NewRangeLocation(ctx.file, insertLine, insertCol, insertLine, insertCol),
			NewText:  newText,
		}},
	}
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

// buildCurlCopyHeredoc builds the COPY heredoc instruction.
// On Linux it includes --chmod=0644; on Windows it omits --chmod.
func buildCurlCopyHeredoc(content string, isWindows bool) string {
	var sb strings.Builder
	sb.WriteString("COPY ")
	if !isWindows {
		sb.WriteString("--chmod=0644 ")
	}
	sb.WriteString("<<EOF ${CURL_HOME}/.curlrc\n")
	sb.WriteString(strings.TrimSuffix(content, "\n"))
	sb.WriteString("\nEOF")
	return sb.String()
}

func (r *PreferCurlConfigRule) resolveConfig(config any) PreferCurlConfigConfig {
	return configutil.Coerce(config, DefaultPreferCurlConfigConfig())
}

func init() {
	rules.Register(NewPreferCurlConfigRule())
}
