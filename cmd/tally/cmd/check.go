package cmd

import (
	stdcontext "context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/context"
	"github.com/tinovyatkin/tally/internal/discovery"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/fix"
	"github.com/tinovyatkin/tally/internal/linter"
	"github.com/tinovyatkin/tally/internal/processor"
	"github.com/tinovyatkin/tally/internal/reporter"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/version"
)

// Exit codes
const (
	ExitSuccess     = 0 // No violations (or below fail-level threshold)
	ExitViolations  = 1 // Violations found at or above fail-level
	ExitConfigError = 2 // Parse or config error
)

func checkCommand() *cli.Command {
	return &cli.Command{
		Name:      "check",
		Usage:     "Check Dockerfile(s) for issues",
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
		},
		Action: runCheck,
	}
}

// lintResults holds the aggregated results of linting all discovered files.
type lintResults struct {
	violations  []rules.Violation
	fileSources map[string][]byte
	fileConfigs map[string]*config.Config
	firstCfg    *config.Config
}

// runCheck is the action handler for the check command.
func runCheck(ctx stdcontext.Context, cmd *cli.Command) error {
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
		fmt.Fprintf(os.Stderr, "Error: failed to discover files: %v\n", err)
		return cli.Exit("", ExitConfigError)
	}

	if len(discovered) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no Dockerfiles found\n")
		return cli.Exit("", ExitConfigError)
	}

	// Lint all discovered files
	res, err := lintFiles(ctx, discovered, cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return cli.Exit("", ExitConfigError)
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
		fixResult, fixErr := applyFixes(ctx, cmd, allViolations, res.fileSources, res.fileConfigs)
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

		validateRuleConfigs(cfg, file)
		validateAIConfig(cfg, file)
		res.fileConfigs[file] = cfg

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
		ToolURI:     "https://github.com/tinovyatkin/tally",
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

	return cfg, nil
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

// validateRuleConfigs validates rule-specific options against each rule's JSON Schema.
// Prints warnings to stderr for invalid configs but does not abort — this allows
// existing configs with unknown keys to continue working while alerting the user.
// Validates all rules including disabled ones to catch typos early.
func validateRuleConfigs(cfg *config.Config, file string) {
	for _, rule := range rules.All() {
		cr, ok := rule.(rules.ConfigurableRule)
		if !ok {
			continue
		}
		opts := cfg.Rules.GetOptions(rule.Metadata().Code)
		if opts == nil {
			continue
		}
		if err := cr.ValidateConfig(opts); err != nil {
			source := file
			if cfg.ConfigFile != "" {
				source = cfg.ConfigFile
			}
			fmt.Fprintf(os.Stderr, "Warning: invalid config for rule %s (%s): %v\n",
				rule.Metadata().Code, source, err)
		}
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
			result[filePath] = modes
		}
	}
	return result
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
