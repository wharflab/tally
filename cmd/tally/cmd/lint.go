package cmd

import (
	"cmp"
	stdcontext "context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/wharflab/tally/internal/ai/autofix"
	"github.com/wharflab/tally/internal/ai/autofixdata"
	"github.com/wharflab/tally/internal/async"
	"github.com/wharflab/tally/internal/config"
	"github.com/wharflab/tally/internal/context"
	"github.com/wharflab/tally/internal/discovery"
	"github.com/wharflab/tally/internal/dockerfile"
	"github.com/wharflab/tally/internal/fileval"
	"github.com/wharflab/tally/internal/fix"
	"github.com/wharflab/tally/internal/linter"
	"github.com/wharflab/tally/internal/processor"
	"github.com/wharflab/tally/internal/registry"
	"github.com/wharflab/tally/internal/reporter"
	"github.com/wharflab/tally/internal/rules"
	"github.com/wharflab/tally/internal/version"
)

// Exit codes
const (
	ExitSuccess     = 0 // No violations (or below fail-level threshold)
	ExitViolations  = 1 // Violations found at or above fail-level
	ExitConfigError = 2 // Parse or config error
	ExitNoFiles     = 3 // No Dockerfiles found (missing file, empty glob, empty directory)
)

func lintCommand() *cli.Command {
	return &cli.Command{
		Name:      "lint",
		Usage:     "Lint Dockerfile(s) for issues",
		ArgsUsage: "[DOCKERFILE...]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to config file (default: auto-discover)",
			},
			&cli.IntFlag{
				Name:    "max-lines",
				Aliases: []string{"l"},
				Usage:   "Maximum number of lines allowed (0 = unlimited)",
				Sources: cli.EnvVars("TALLY_RULES_MAX_LINES_MAX"),
			},
			&cli.BoolFlag{
				Name:    "skip-blank-lines",
				Usage:   "Exclude blank lines from the line count",
				Sources: cli.EnvVars("TALLY_RULES_MAX_LINES_SKIP_BLANK_LINES"),
			},
			&cli.BoolFlag{
				Name:    "skip-comments",
				Usage:   "Exclude comment lines from the line count",
				Sources: cli.EnvVars("TALLY_RULES_MAX_LINES_SKIP_COMMENTS"),
			},
			&cli.StringFlag{
				Name:    "format",
				Aliases: []string{"f"},
				Usage:   "Output format: text, json, sarif, github-actions",
				Sources: cli.EnvVars("TALLY_FORMAT", "TALLY_OUTPUT_FORMAT"),
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "Output path: stdout, stderr, or file path",
				Sources: cli.EnvVars("TALLY_OUTPUT_PATH"),
			},
			&cli.BoolFlag{
				Name:    "no-color",
				Usage:   "Disable colored output",
				Sources: cli.EnvVars("NO_COLOR"),
			},
			&cli.BoolFlag{
				Name:    "show-source",
				Usage:   "Show source code snippets (default: true)",
				Value:   true,
				Sources: cli.EnvVars("TALLY_OUTPUT_SHOW_SOURCE"),
			},
			&cli.BoolFlag{
				Name:  "hide-source",
				Usage: "Hide source code snippets",
			},
			&cli.StringFlag{
				Name:    "fail-level",
				Usage:   "Minimum severity to cause non-zero exit: error, warning, info, style, none",
				Sources: cli.EnvVars("TALLY_OUTPUT_FAIL_LEVEL"),
			},
			&cli.BoolFlag{
				Name:    "no-inline-directives",
				Usage:   "Disable processing of inline ignore directives",
				Sources: cli.EnvVars("TALLY_NO_INLINE_DIRECTIVES"),
			},
			&cli.BoolFlag{
				Name:    "warn-unused-directives",
				Usage:   "Warn about unused ignore directives",
				Sources: cli.EnvVars("TALLY_INLINE_DIRECTIVES_WARN_UNUSED"),
			},
			&cli.BoolFlag{
				Name:    "require-reason",
				Usage:   "Warn about ignore directives without reason= explanation",
				Sources: cli.EnvVars("TALLY_INLINE_DIRECTIVES_REQUIRE_REASON"),
			},
			&cli.StringSliceFlag{
				Name:    "exclude",
				Usage:   "Glob pattern to exclude files (can be repeated)",
				Sources: cli.EnvVars("TALLY_EXCLUDE"),
			},
			&cli.StringSliceFlag{
				Name:    "select",
				Usage:   "Enable specific rules (pattern: rule-code, namespace/*, *)",
				Sources: cli.EnvVars("TALLY_RULES_SELECT"),
			},
			&cli.StringSliceFlag{
				Name:    "ignore",
				Usage:   "Disable specific rules (pattern: rule-code, namespace/*, *)",
				Sources: cli.EnvVars("TALLY_RULES_IGNORE"),
			},
			&cli.StringFlag{
				Name:    "context",
				Usage:   "Build context directory for context-aware rules",
				Sources: cli.EnvVars("TALLY_CONTEXT"),
			},
			&cli.StringFlag{
				Name:    "slow-checks",
				Usage:   "Slow checks mode: auto, on, off",
				Sources: cli.EnvVars("TALLY_SLOW_CHECKS"),
			},
			&cli.StringFlag{
				Name:    "slow-checks-timeout",
				Usage:   "Timeout for slow checks (e.g., 20s)",
				Sources: cli.EnvVars("TALLY_SLOW_CHECKS_TIMEOUT"),
			},
			&cli.BoolFlag{
				Name:    "fix",
				Usage:   "Apply all safe fixes automatically",
				Sources: cli.EnvVars("TALLY_FIX"),
			},
			&cli.StringSliceFlag{
				Name:    "fix-rule",
				Usage:   "Only fix specific rules (can be repeated)",
				Sources: cli.EnvVars("TALLY_FIX_RULE"),
			},
			&cli.BoolFlag{
				Name:    "fix-unsafe",
				Usage:   "Also apply suggestion/unsafe fixes (requires --fix)",
				Sources: cli.EnvVars("TALLY_FIX_UNSAFE"),
			},
			&cli.BoolFlag{
				Name:    "ai",
				Usage:   "Enable AI AutoFix (requires an ACP agent command)",
				Sources: cli.EnvVars("TALLY_AI_ENABLED"),
			},
			&cli.StringFlag{
				Name:    "acp-command",
				Usage:   "ACP agent command line (e.g. \"gemini --experimental-acp\")",
				Sources: cli.EnvVars("TALLY_ACP_COMMAND"),
			},
			&cli.StringFlag{
				Name:    "ai-timeout",
				Usage:   "Per-fix AI timeout (e.g., 90s)",
				Sources: cli.EnvVars("TALLY_AI_TIMEOUT"),
			},
			&cli.IntFlag{
				Name:    "ai-max-input-bytes",
				Usage:   "Maximum prompt size in bytes",
				Sources: cli.EnvVars("TALLY_AI_MAX_INPUT_BYTES"),
			},
			&cli.BoolFlag{
				Name:    "ai-redact-secrets",
				Usage:   "Redact obvious secrets before sending content to the agent",
				Value:   true,
				Sources: cli.EnvVars("TALLY_AI_REDACT_SECRETS"),
			},
		},
		Action: runLint,
	}
}

// lintResults holds the aggregated results of linting all discovered files.
type lintResults struct {
	violations  []rules.Violation
	asyncPlans  []async.CheckRequest
	fileSources map[string][]byte
	fileConfigs map[string]*config.Config
	firstCfg    *config.Config
}

func collectRegistryInsights(
	plans []async.CheckRequest,
	result *async.RunResult,
) map[string][]autofixdata.RegistryInsight {
	if result == nil || len(result.Resolved) == 0 || len(plans) == 0 {
		return nil
	}

	byFile := make(map[string]map[string]autofixdata.RegistryInsight)

	for _, req := range plans {
		if req.ResolverID != registry.RegistryResolverID() {
			continue
		}

		data, ok := req.Data.(*registry.ResolveRequest)
		if !ok || data == nil {
			continue
		}

		resolved, ok := result.Resolved[async.ResolutionKey{ResolverID: req.ResolverID, Key: req.Key}]
		if !ok {
			continue
		}

		fileKey := filepath.ToSlash(req.File)
		if fileKey == "" {
			continue
		}

		stageKey := strconv.Itoa(req.StageIndex) + "|" + req.Key
		insight := autofixdata.RegistryInsight{
			StageIndex:         req.StageIndex,
			Ref:                data.Ref,
			RequestedPlatform:  data.Platform,
			ResolvedPlatform:   "",
			Digest:             "",
			AvailablePlatforms: nil,
		}

		switch v := resolved.(type) {
		case *registry.ImageConfig:
			if v != nil {
				insight.ResolvedPlatform = formatPlatformParts(v.OS, v.Arch, v.Variant)
				insight.Digest = v.Digest
			}
		case *registry.PlatformMismatchError:
			if v != nil && len(v.Available) > 0 {
				insight.AvailablePlatforms = append([]string(nil), v.Available...)
			}
		}

		m := byFile[fileKey]
		if m == nil {
			m = make(map[string]autofixdata.RegistryInsight)
			byFile[fileKey] = m
		}
		m[stageKey] = insight
	}

	if len(byFile) == 0 {
		return nil
	}

	out := make(map[string][]autofixdata.RegistryInsight, len(byFile))
	for file, m := range byFile {
		list := make([]autofixdata.RegistryInsight, 0, len(m))
		for _, ins := range m {
			list = append(list, ins)
		}
		slices.SortFunc(list, func(a, b autofixdata.RegistryInsight) int {
			if d := cmp.Compare(a.StageIndex, b.StageIndex); d != 0 {
				return d
			}
			if d := strings.Compare(a.Ref, b.Ref); d != 0 {
				return d
			}
			return strings.Compare(a.RequestedPlatform, b.RequestedPlatform)
		})
		out[file] = list
	}

	return out
}

func formatPlatformParts(osName, arch, variant string) string {
	s := osName + "/" + arch
	if variant != "" {
		s += "/" + variant
	}
	return s
}

// runLint is the action handler for the lint command.
func runLint(ctx stdcontext.Context, cmd *cli.Command) error {
	inputs := cmd.Args().Slice()
	if len(inputs) == 0 {
		inputs = []string{"."}
	}

	// Discover files using the discovery package
	discoveryOpts := discovery.Options{
		Patterns:        discovery.DefaultPatterns(),
		ExcludePatterns: cmd.StringSlice("exclude"),
		ContextDir:      cmd.String("context"),
	}

	discovered, err := discovery.Discover(inputs, discoveryOpts)
	if err != nil {
		var notFound *discovery.FileNotFoundError
		if errors.As(err, &notFound) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", notFound)
			return cli.Exit("", ExitNoFiles)
		}
		fmt.Fprintf(os.Stderr, "Error: failed to discover files: %v\n", err)
		return cli.Exit("", ExitConfigError)
	}

	if len(discovered) == 0 {
		reportNoFilesFound(inputs)
		return cli.Exit("", ExitNoFiles)
	}

	// Lint all discovered files
	res, err := lintFiles(ctx, discovered, cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return cli.Exit("", ExitConfigError)
	}

	// Execute async checks if enabled and plans exist.
	var (
		asyncResult *async.RunResult
		asyncPlans  []async.CheckRequest
	)
	if len(res.asyncPlans) > 0 {
		asyncResult, asyncPlans = runAsyncChecks(ctx, res)
		if asyncResult != nil {
			res.violations = mergeAsyncViolations(res.violations, asyncResult)
		}
	}

	// Build processor chain for violation processing.
	// Each file gets its own config for rule enable/disable, severity, etc.
	chain, inlineFilter := linter.CLIProcessors()
	procCtx := processor.NewContext(res.fileConfigs, res.firstCfg, res.fileSources)
	allViolations := chain.Process(res.violations, procCtx)

	// Add any additional violations from the inline directive filter
	// (parse errors, unused directives, missing reasons)
	additionalViolations := inlineFilter.AdditionalViolations()
	if len(additionalViolations) > 0 {
		additionalViolations = processor.NewPathNormalization().Process(additionalViolations, procCtx)
		additionalViolations = processor.NewSnippetAttachment().Process(additionalViolations, procCtx)
		allViolations = append(allViolations, additionalViolations...)
		allViolations = reporter.SortViolations(allViolations)
	}

	// Apply fixes if --fix flag is set
	if cmd.Bool("fix-unsafe") && !cmd.Bool("fix") {
		fmt.Fprintf(os.Stderr, "Warning: --fix-unsafe has no effect without --fix\n")
	}
	if cmd.Bool("fix") {
		fixResult, fixErr := applyFixes(ctx, cmd, allViolations, res.fileSources, res.fileConfigs, asyncPlans, asyncResult)
		if fixErr != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to apply fixes: %v\n", fixErr)
			return cli.Exit("", ExitConfigError)
		}

		if fixResult.TotalApplied() > 0 {
			fmt.Fprintf(os.Stderr, "Fixed %d issues in %d files\n",
				fixResult.TotalApplied(), fixResult.FilesModified())
		}
		if fixResult.TotalSkipped() > 0 {
			fmt.Fprintf(os.Stderr, "Skipped %d fixes\n", fixResult.TotalSkipped())
			reportSkippedFixes(fixResult)
		}

		allViolations = filterFixedViolations(allViolations, fixResult)
	}

	return writeReport(cmd, res.firstCfg, allViolations, res.fileSources, len(discovered))
}

// lintFiles runs the lint pipeline on each discovered file and aggregates results.
func lintFiles(ctx stdcontext.Context, discovered []discovery.DiscoveredFile, cmd *cli.Command) (*lintResults, error) {
	res := &lintResults{
		fileSources: make(map[string][]byte),
		fileConfigs: make(map[string]*config.Config),
	}

	for _, df := range discovered {
		file := df.Path

		cfg, err := loadConfigForFile(cmd, file)
		if err != nil {
			return nil, fmt.Errorf("failed to load config for %s: %w", file, err)
		}

		validateAIConfig(cfg, file)
		validateDurationConfigs(cfg, file)
		res.fileConfigs[file] = cfg

		if err := fileval.ValidateFile(file, cfg.FileValidation.MaxFileSize); err != nil {
			return nil, fmt.Errorf("failed to lint %s: %w", file, err)
		}

		if res.firstCfg == nil {
			res.firstCfg = cfg
		}

		// Build context for context-aware rules (e.g. .dockerignore checks).
		// This requires parsing the Dockerfile first to extract heredoc files.
		var buildCtx rules.BuildContext
		if df.ContextDir != "" {
			parseResult, parseErr := dockerfile.ParseFile(ctx, file, cfg)
			if parseErr == nil {
				buildCtx, err = context.New(df.ContextDir, file,
					context.WithHeredocFiles(extractHeredocFiles(parseResult)))
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to create build context: %v\n", err)
				}
			}
		}

		result, err := linter.LintFile(linter.Input{
			FilePath:     file,
			Config:       cfg,
			BuildContext: buildCtx,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to lint %s: %w", file, err)
		}

		res.fileSources[file] = result.ParseResult.Source
		res.violations = append(res.violations, result.Violations...)
		res.asyncPlans = append(res.asyncPlans, result.AsyncPlan...)
	}

	return res, nil
}

// writeReport formats and writes the violation report.
func writeReport(
	cmd *cli.Command, cfg *config.Config, violations []rules.Violation,
	fileSources map[string][]byte, filesScanned int,
) error {
	outCfg := getOutputConfig(cmd, cfg)

	formatType, err := reporter.ParseFormat(outCfg.format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return cli.Exit("", ExitConfigError)
	}

	writer, closeWriter, err := reporter.GetWriter(outCfg.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return cli.Exit("", ExitConfigError)
	}
	defer func() {
		if err := closeWriter(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close output: %v\n", err)
		}
	}()

	opts := reporter.Options{
		Format:      formatType,
		Writer:      writer,
		ShowSource:  outCfg.showSource,
		ToolName:    "tally",
		ToolVersion: version.Version(),
		ToolURI:     "https://github.com/wharflab/tally",
	}

	if cmd.IsSet("no-color") && cmd.Bool("no-color") {
		noColor := false
		opts.Color = &noColor
	}

	rep, err := reporter.New(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create reporter: %v\n", err)
		return cli.Exit("", ExitConfigError)
	}

	rulesEnabled := len(linter.EnabledRuleCodes(cfg))
	metadata := reporter.ReportMetadata{
		FilesScanned: filesScanned,
		RulesEnabled: rulesEnabled,
	}

	if err := rep.Report(violations, fileSources, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to write output: %v\n", err)
		return cli.Exit("", ExitConfigError)
	}

	exitCode := determineExitCode(violations, outCfg.failLevel)
	if exitCode != ExitSuccess {
		return cli.Exit("", exitCode)
	}

	return nil
}

// loadConfigForFile loads configuration for a target file, applying CLI overrides.
func loadConfigForFile(cmd *cli.Command, targetPath string) (*config.Config, error) {
	var cfg *config.Config
	var err error

	// Check if a specific config file was provided
	if configPath := cmd.String("config"); configPath != "" {
		// Load from specific config file
		cfg, err = config.LoadFromFile(configPath)
		if err != nil {
			return nil, err
		}
	} else {
		// Auto-discover config file based on target path
		cfg, err = config.Load(targetPath)
		if err != nil {
			return nil, err
		}
	}

	// Apply CLI flag overrides for max-lines rule
	// Only override if the flag was explicitly set
	if cmd.IsSet("max-lines") || cmd.IsSet("skip-blank-lines") || cmd.IsSet("skip-comments") {
		// Get current options or defaults
		opts := cfg.Rules.GetOptions("tally/max-lines")
		if opts == nil {
			opts = make(map[string]any)
		}

		if cmd.IsSet("max-lines") {
			opts["max"] = cmd.Int("max-lines")
		}
		if cmd.IsSet("skip-blank-lines") {
			opts["skip-blank-lines"] = cmd.Bool("skip-blank-lines")
		}
		if cmd.IsSet("skip-comments") {
			opts["skip-comments"] = cmd.Bool("skip-comments")
		}

		// Get existing config or create new
		ruleCfg := cfg.Rules.Get("tally/max-lines")
		if ruleCfg != nil {
			ruleCfg.Options = opts
			cfg.Rules.Set("tally/max-lines", *ruleCfg)
		} else {
			cfg.Rules.Set("tally/max-lines", config.RuleConfig{Options: opts})
		}
	}

	// Apply rule selection overrides from CLI flags
	if cmd.IsSet("select") {
		cfg.Rules.Include = append(cfg.Rules.Include, cmd.StringSlice("select")...)
	}
	if cmd.IsSet("ignore") {
		cfg.Rules.Exclude = append(cfg.Rules.Exclude, cmd.StringSlice("ignore")...)
	}

	// Output settings are handled in getOutputConfig to avoid duplication

	// --no-inline-directives flag inverts the enabled setting
	if cmd.IsSet("no-inline-directives") {
		cfg.InlineDirectives.Enabled = !cmd.Bool("no-inline-directives")
	}

	if cmd.IsSet("warn-unused-directives") {
		cfg.InlineDirectives.WarnUnused = cmd.Bool("warn-unused-directives")
	}

	if cmd.IsSet("require-reason") {
		cfg.InlineDirectives.RequireReason = cmd.Bool("require-reason")
	}

	// Apply slow-checks CLI overrides
	if cmd.IsSet("slow-checks") {
		cfg.SlowChecks.Mode = cmd.String("slow-checks")
	}
	if cmd.IsSet("slow-checks-timeout") {
		cfg.SlowChecks.Timeout = cmd.String("slow-checks-timeout")
	}

	// Apply AI CLI overrides
	if cmd.IsSet("ai") {
		cfg.AI.Enabled = cmd.Bool("ai")
	}
	if cmd.IsSet("acp-command") {
		argv, err := parseACPCmd(cmd.String("acp-command"))
		if err != nil {
			return nil, err
		}
		cfg.AI.Command = argv
		cfg.AI.Enabled = true
	}
	if cmd.IsSet("ai-timeout") {
		cfg.AI.Timeout = cmd.String("ai-timeout")
	}
	if cmd.IsSet("ai-max-input-bytes") {
		cfg.AI.MaxInputBytes = cmd.Int("ai-max-input-bytes")
	}
	if cmd.IsSet("ai-redact-secrets") {
		cfg.AI.RedactSecrets = cmd.Bool("ai-redact-secrets")
	}

	return cfg, nil
}

func parseACPCmd(commandLine string) ([]string, error) {
	fields, err := splitCommandLine(commandLine)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 || fields[0] == "" {
		return nil, errors.New("acp-command is empty")
	}
	return fields, nil
}

func splitCommandLine(commandLine string) ([]string, error) {
	var s commandLineSplitter
	for i := range len(commandLine) {
		var next byte
		hasNext := false
		if i+1 < len(commandLine) {
			next = commandLine[i+1]
			hasNext = true
		}
		s.consume(commandLine[i], next, hasNext)
	}
	return s.finish()
}

type commandLineSplitter struct {
	out []string
	cur strings.Builder

	inArg    bool
	inSingle bool
	inDouble bool
	escaped  bool
}

func (s *commandLineSplitter) flush() {
	if !s.inArg {
		return
	}
	s.out = append(s.out, s.cur.String())
	s.cur.Reset()
	s.inArg = false
}

func (s *commandLineSplitter) consume(ch, next byte, hasNext bool) {
	switch {
	case s.escaped:
		s.consumeEscaped(ch)
	case s.inSingle:
		s.consumeSingle(ch)
	case s.inDouble:
		s.consumeDouble(ch, next, hasNext)
	default:
		s.consumePlain(ch, next, hasNext)
	}
}

func (s *commandLineSplitter) consumeEscaped(ch byte) {
	s.cur.WriteByte(ch)
	s.escaped = false
	s.inArg = true
}

func (s *commandLineSplitter) consumeSingle(ch byte) {
	if ch == '\'' {
		s.inSingle = false
		s.inArg = true
		return
	}
	s.cur.WriteByte(ch)
	s.inArg = true
}

func (s *commandLineSplitter) consumeDouble(ch, next byte, hasNext bool) {
	switch ch {
	case '"':
		s.inDouble = false
		s.inArg = true
	case '\\':
		if hasNext && shouldEscapeNextByte(next) {
			s.escaped = true
			s.inArg = true
			return
		}
		s.cur.WriteByte('\\')
		s.inArg = true
	default:
		s.cur.WriteByte(ch)
		s.inArg = true
	}
}

func (s *commandLineSplitter) consumePlain(ch, next byte, hasNext bool) {
	switch ch {
	case ' ', '\t', '\n', '\r':
		s.flush()
	case '\'':
		s.inSingle = true
		s.inArg = true
	case '"':
		s.inDouble = true
		s.inArg = true
	case '\\':
		if hasNext && shouldEscapeNextByte(next) {
			s.escaped = true
			s.inArg = true
			return
		}
		s.cur.WriteByte('\\')
		s.inArg = true
	default:
		s.cur.WriteByte(ch)
		s.inArg = true
	}
}

func shouldEscapeNextByte(next byte) bool {
	switch next {
	case '"', '\'', '\\', ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func (s *commandLineSplitter) finish() ([]string, error) {
	if s.escaped {
		return nil, errors.New("acp-command has a trailing backslash escape")
	}
	if s.inSingle {
		return nil, errors.New("acp-command has an unterminated single quote")
	}
	if s.inDouble {
		return nil, errors.New("acp-command has an unterminated double quote")
	}

	s.flush()
	return s.out, nil
}

// outputConfig holds output configuration values.
type outputConfig struct {
	format     string
	path       string
	showSource bool
	failLevel  string
}

// getOutputConfig returns output configuration from CLI flags and config.
func getOutputConfig(cmd *cli.Command, cfg *config.Config) outputConfig {
	// Start with defaults
	oc := outputConfig{
		format:     "text",
		path:       "stdout",
		showSource: true,
		failLevel:  "style",
	}

	if cfg != nil {
		// Apply config values
		if cfg.Output.Format != "" {
			oc.format = cfg.Output.Format
		}

		if cfg.Output.Path != "" {
			oc.path = cfg.Output.Path
		}

		oc.showSource = cfg.Output.ShowSource

		if cfg.Output.FailLevel != "" {
			oc.failLevel = cfg.Output.FailLevel
		}
	}

	// CLI flags take precedence
	if cmd.IsSet("format") {
		oc.format = cmd.String("format")
	}

	if cmd.IsSet("output") {
		oc.path = cmd.String("output")
	}

	if cmd.IsSet("show-source") {
		oc.showSource = cmd.Bool("show-source")
	}

	if cmd.IsSet("hide-source") && cmd.Bool("hide-source") {
		oc.showSource = false
	}

	if cmd.IsSet("fail-level") {
		oc.failLevel = cmd.String("fail-level")
	}

	return oc
}

// determineExitCode returns the appropriate exit code based on violations and fail-level.
func determineExitCode(violations []rules.Violation, failLevel string) int {
	// "none" means never fail due to violations
	if failLevel == "none" {
		return ExitSuccess
	}

	// Parse fail-level first to catch config errors even with no violations
	threshold, err := parseFailLevel(failLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid --fail-level %q\n", failLevel)
		return ExitConfigError
	}

	if len(violations) == 0 {
		return ExitSuccess
	}

	// Check if any violation meets or exceeds the threshold
	for _, v := range violations {
		if v.Severity.IsAtLeast(threshold) {
			return ExitViolations
		}
	}

	return ExitSuccess
}

// parseFailLevel parses a fail-level string to a Severity.
func parseFailLevel(level string) (rules.Severity, error) {
	switch level {
	case "", "style":
		// Default to "style" (any violation fails)
		return rules.SeverityStyle, nil
	default:
		return rules.ParseSeverity(level)
	}
}

// validateAIConfig validates top-level AI configuration.
// Prints warnings to stderr but does not abort.
func validateAIConfig(cfg *config.Config, file string) {
	if cfg == nil || !cfg.AI.Enabled {
		return
	}

	if len(cfg.AI.Command) == 0 {
		source := file
		if cfg.ConfigFile != "" {
			source = cfg.ConfigFile
		}
		fmt.Fprintf(os.Stderr, "Warning: ai.enabled=true but ai.command is empty (%s)\n", source)
	}
}

// validateDurationConfigs validates duration string fields at config load time
// so the user sees a clear warning instead of silent fallback to defaults.
func validateDurationConfigs(cfg *config.Config, file string) {
	source := file
	if cfg.ConfigFile != "" {
		source = cfg.ConfigFile
	}

	if t := cfg.SlowChecks.Timeout; t != "" {
		if _, err := time.ParseDuration(t); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid slow-checks.timeout %q (%s): %v\n", t, source, err)
		}
	}
	if t := cfg.AI.Timeout; t != "" {
		if _, err := time.ParseDuration(t); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid ai.timeout %q (%s): %v\n", t, source, err)
		}
	}
}

// extractHeredocFiles extracts virtual file paths from heredoc COPY/ADD commands.
// These are inline files created by heredoc syntax that should not be checked
// against .dockerignore.
func extractHeredocFiles(parseResult *dockerfile.ParseResult) map[string]bool {
	return dockerfile.ExtractHeredocFiles(parseResult.Stages)
}

// applyFixes applies automatic fixes to violations that have suggested fixes.
// fileConfigs maps file paths to their per-file configs (for per-file fix modes).
func applyFixes(
	ctx stdcontext.Context,
	cmd *cli.Command,
	violations []rules.Violation,
	sources map[string][]byte,
	fileConfigs map[string]*config.Config,
	asyncPlans []async.CheckRequest,
	asyncResult *async.RunResult,
) (*fix.Result, error) {
	// Determine safety threshold
	safetyThreshold := fix.FixSafe
	if cmd.Bool("fix-unsafe") {
		safetyThreshold = fix.FixUnsafe
	}

	// Get rule filter
	ruleFilter := cmd.StringSlice("fix-rule")

	// Build per-file fix modes from fileConfigs
	fixModes := buildPerFileFixModes(fileConfigs)

	// Register AI resolver only when AI is enabled for at least one file config.
	aiEnabled := false
	normalizedConfigs := make(map[string]*config.Config, len(fileConfigs))
	for path, cfg := range fileConfigs {
		normalizedConfigs[filepath.ToSlash(path)] = cfg
		if cfg != nil && cfg.AI.Enabled {
			aiEnabled = true
		}
	}
	if aiEnabled {
		autofix.Register()
	}

	registryInsightsByFile := collectRegistryInsights(asyncPlans, asyncResult)

	// Enrich AI resolver requests with per-file config + outer fix context.
	fixCtx := autofixdata.FixContext{
		SafetyThreshold: safetyThreshold,
		RuleFilter:      ruleFilter,
		FixModes:        fixModes,
	}
	for i := range violations {
		v := &violations[i]
		if v.SuggestedFix == nil || !v.SuggestedFix.NeedsResolve {
			continue
		}
		if v.SuggestedFix.ResolverID != autofixdata.ResolverID {
			continue
		}
		req, ok := v.SuggestedFix.ResolverData.(interface {
			SetConfig(cfg *config.Config)
			SetFixContext(ctx autofixdata.FixContext)
		})
		if !ok {
			continue
		}
		cfg := normalizedConfigs[filepath.ToSlash(v.File())]
		req.SetConfig(cfg)
		req.SetFixContext(fixCtx)

		if setter, ok := v.SuggestedFix.ResolverData.(interface {
			SetRegistryInsights(insights []autofixdata.RegistryInsight)
		}); ok {
			setter.SetRegistryInsights(registryInsightsByFile[filepath.ToSlash(v.File())])
		}
	}

	aiFixes, maxAITimeout := planAcpFixSpinner(violations, safetyThreshold, ruleFilter, fixModes, normalizedConfigs)
	stopSpinner := startAcpFixSpinner(aiFixes, maxAITimeout)
	defer stopSpinner()

	fixer := &fix.Fixer{
		SafetyThreshold: safetyThreshold,
		RuleFilter:      ruleFilter,
		FixModes:        fixModes,
		Concurrency:     4,
	}

	result, err := fixer.Apply(ctx, violations, sources)
	if err != nil {
		return nil, err
	}

	// Write modified files (preserve original permissions)
	for _, fc := range result.Changes {
		if !fc.HasChanges() {
			continue
		}
		mode := os.FileMode(0o644)
		if info, err := os.Stat(fc.Path); err == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(fc.Path, fc.ModifiedContent, mode); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", fc.Path, err)
		}
	}

	return result, nil
}

// buildPerFileFixModes builds a per-file map of fix modes from fileConfigs.
// Returns map[filePath]map[ruleCode]FixMode.
func buildPerFileFixModes(fileConfigs map[string]*config.Config) map[string]map[string]fix.FixMode {
	result := make(map[string]map[string]fix.FixMode)
	for filePath, cfg := range fileConfigs {
		if cfg == nil {
			continue
		}
		modes := fix.BuildFixModes(cfg)
		if len(modes) > 0 {
			result[filepath.Clean(filePath)] = modes
		}
	}
	return result
}

// runAsyncChecks executes async check plans if slow checks are enabled.
// Returns nil if slow checks are disabled or no plans exist.
// Respects per-file slow-checks configuration from res.fileConfigs.
func runAsyncChecks(ctx stdcontext.Context, res *lintResults) (*async.RunResult, []async.CheckRequest) {
	if len(res.asyncPlans) == 0 {
		return nil, nil
	}

	plans, maxTimeout := filterAsyncPlans(res)
	if len(plans) == 0 {
		return nil, nil
	}

	// Register the registry resolver (once per invocation).
	if registry.NewDefaultResolver == nil {
		fmt.Fprintf(os.Stderr, "note: slow checks not available (missing build tags)\n")
		return nil, nil
	}
	imgResolver := registry.NewDefaultResolver()
	asyncImgResolver := registry.NewAsyncImageResolver(imgResolver)

	rt := &async.Runtime{
		Concurrency: 4,
		Timeout:     maxTimeout,
		Resolvers: map[string]async.Resolver{
			asyncImgResolver.ID(): asyncImgResolver,
		},
	}

	result := rt.Run(ctx, plans)
	reportSkipped(result)
	return result, plans
}

// filterAsyncPlans applies per-file slow-checks policy to async plans.
// Returns the filtered plans and the maximum timeout across all enabled files.
func filterAsyncPlans(res *lintResults) ([]async.CheckRequest, time.Duration) {
	// Pre-apply severity overrides and enable filter so fail-fast only considers
	// violations from rules the user has actually enabled (respecting --select,
	// --ignore, and severity overrides).
	procCtx := processor.NewContext(res.fileConfigs, res.firstCfg, res.fileSources)
	filtered := processor.NewSeverityOverride().Process(res.violations, procCtx)
	filtered = processor.NewEnableFilter().Process(filtered, procCtx)
	errorFiles := filesWithErrors(filtered)
	maxTimeout := 20 * time.Second
	var plans []async.CheckRequest
	var skippedAuto int

	for _, req := range res.asyncPlans {
		cfg := res.fileConfigs[req.File]
		if cfg == nil {
			cfg = res.firstCfg
		}
		if cfg == nil {
			continue
		}

		slowCfg := cfg.SlowChecks
		if !config.SlowChecksEnabled(slowCfg.Mode) {
			if slowCfg.Mode == "auto" {
				skippedAuto++
			}
			continue
		}

		// Per-file fail-fast: skip if fast rules produced SeverityError.
		if slowCfg.FailFast && errorFiles[req.File] {
			continue
		}

		// Apply per-file timeout to the request.
		if d, err := time.ParseDuration(slowCfg.Timeout); err == nil && d > 0 {
			req.Timeout = d
			if d > maxTimeout {
				maxTimeout = d
			}
		}

		plans = append(plans, req)
	}

	if skippedAuto > 0 {
		ciName := config.CIName()
		if ciName != "" {
			fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (%s detected; use --slow-checks=on to enable)\n", skippedAuto, ciName)
		} else {
			fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (use --slow-checks=on to enable)\n", skippedAuto)
		}
	}

	return plans, maxTimeout
}

// reportSkipped prints summary notes for skipped async checks.
func reportSkipped(result *async.RunResult) {
	if len(result.Skipped) == 0 {
		return
	}
	counts := make(map[async.SkipReason]int)
	for _, s := range result.Skipped {
		counts[s.Reason]++
	}
	if n := counts[async.SkipTimeout]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) timed out (increase --slow-checks-timeout)\n", n)
	}
	if n := counts[async.SkipNetwork]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (registry unreachable or rate-limited)\n", n)
	}
	if n := counts[async.SkipAuth]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (authentication failed)\n", n)
	}
	if n := counts[async.SkipNotFound]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped (image not found)\n", n)
	}
	if n := counts[async.SkipResolverErr]; n > 0 {
		fmt.Fprintf(os.Stderr, "note: %d slow check(s) skipped due to errors\n", n)
	}
}

func reportSkippedFixes(result *fix.Result) {
	if result == nil || result.TotalSkipped() == 0 {
		return
	}

	type skippedFixInfo struct {
		filePath string
		ruleCode string
		errorMsg string
	}

	var (
		aiTimeouts int
		aiErrors   int
		otherErrs  int
		samples    []skippedFixInfo
	)

	for _, fc := range result.Changes {
		if fc == nil {
			continue
		}
		for _, s := range fc.FixesSkipped {
			if s.Reason != fix.SkipResolveError || s.Error == "" {
				continue
			}

			isAI := strings.Contains(s.Error, "ai-autofix") || strings.Contains(s.Error, "acp ")
			if isAI {
				if strings.Contains(s.Error, stdcontext.DeadlineExceeded.Error()) {
					aiTimeouts++
				} else {
					aiErrors++
				}
			} else {
				otherErrs++
			}

			if len(samples) < 5 {
				samples = append(samples, skippedFixInfo{
					filePath: fc.Path,
					ruleCode: s.RuleCode,
					errorMsg: compactSingleLine(s.Error, 500),
				})
			}
		}
	}

	if aiTimeouts > 0 {
		fmt.Fprintf(os.Stderr, "note: %d AI fix(es) timed out (increase --ai-timeout or ai.timeout)\n", aiTimeouts)
	}
	if aiErrors > 0 {
		fmt.Fprintf(os.Stderr, "note: %d AI fix(es) failed (see details below)\n", aiErrors)
	}
	if otherErrs > 0 {
		fmt.Fprintf(os.Stderr, "note: %d fix(es) skipped due to resolver errors\n", otherErrs)
	}

	for _, s := range samples {
		fmt.Fprintf(os.Stderr, "note: skipped fix %s (%s): %s\n", s.ruleCode, s.filePath, s.errorMsg)
	}
}

func compactSingleLine(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "; ")
	s = strings.TrimSpace(s)
	if maxLen > 0 && len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}

// filesWithErrors returns a set of files that have SeverityError violations.
func filesWithErrors(violations []rules.Violation) map[string]bool {
	m := make(map[string]bool)
	for _, v := range violations {
		if v.Severity == rules.SeverityError {
			m[v.File()] = true
		}
	}
	return m
}

// mergeAsyncViolations merges async results into the fast violations.
// For rules with async resolution (e.g. UndefinedVar), fast violations for
// a (rule, file, stage) triple are replaced by async results when the async
// check completes — even when it produces zero violations (eliminating false
// positives from the fast path). Stage-level granularity ensures that fast
// violations from non-async stages in the same file are preserved.
func mergeAsyncViolations(fast []rules.Violation, asyncResult *async.RunResult) []rules.Violation {
	if asyncResult == nil {
		return fast
	}

	// Convert []any to []rules.Violation.
	var asyncViolations []rules.Violation
	for _, v := range asyncResult.Violations {
		if viol, ok := v.(rules.Violation); ok {
			asyncViolations = append(asyncViolations, viol)
		}
	}

	if len(asyncResult.Completed) == 0 && len(asyncViolations) == 0 {
		return fast
	}

	// Build set of (rule, file, stage) triples that completed async resolution.
	// Fast violations for these triples are replaced by async results.
	// Stage-level granularity ensures that fast violations from non-async stages
	// in the same file are preserved.
	type ruleFileStage struct {
		ruleCode   string
		file       string
		stageIndex int
	}
	completedSet := make(map[ruleFileStage]bool)
	for _, c := range asyncResult.Completed {
		completedSet[ruleFileStage{ruleCode: c.RuleCode, file: c.File, stageIndex: c.StageIndex}] = true
	}

	// Filter out fast violations that were superseded by async results.
	var merged []rules.Violation
	for _, v := range fast {
		if completedSet[ruleFileStage{ruleCode: v.RuleCode, file: v.File(), stageIndex: v.StageIndex}] {
			continue // replaced by async result
		}
		merged = append(merged, v)
	}

	// Append all async violations.
	merged = append(merged, asyncViolations...)
	return merged
}

// filterFixedViolations removes violations that were fixed from the list.
func filterFixedViolations(violations []rules.Violation, fixResult *fix.Result) []rules.Violation {
	// Build set of fixed locations (include column to handle multiple violations on same line)
	type locKey struct {
		file string
		line int
		col  int
		code string
	}
	fixed := make(map[locKey]bool)
	for _, fc := range fixResult.Changes {
		for _, af := range fc.FixesApplied {
			fixed[locKey{
				// Use ToSlash for consistent cross-platform path matching
				// Violations use forward slashes (PathNormalization processor)
				file: filepath.ToSlash(fc.Path),
				line: af.Location.Start.Line,
				col:  af.Location.Start.Column,
				code: af.RuleCode,
			}] = true
		}
	}

	// Filter violations
	var remaining []rules.Violation
	for _, v := range violations {
		key := locKey{
			file: filepath.ToSlash(v.File()),
			line: v.Line(),
			col:  v.Location.Start.Column,
			code: v.RuleCode,
		}
		if !fixed[key] {
			remaining = append(remaining, v)
		}
	}
	return remaining
}

// reportNoFilesFound prints a context-aware message when no Dockerfiles are found.
func reportNoFilesFound(inputs []string) {
	for _, input := range inputs {
		if discovery.ContainsGlobChars(input) {
			fmt.Fprintf(os.Stderr, "Error: no Dockerfiles matched pattern: %s\n", input)
			return
		}
	}

	// For directory inputs, resolve to absolute path so the user knows exactly
	// which directory was scanned.
	for _, input := range inputs {
		abs, err := filepath.Abs(input)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err == nil && info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: no Dockerfile or Containerfile found in %s\n", abs)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Error: no Dockerfiles found\n")
}
