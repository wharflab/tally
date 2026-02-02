package cmd

import (
	stdcontext "context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/context"
	"github.com/tinovyatkin/tally/internal/directive"
	"github.com/tinovyatkin/tally/internal/discovery"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/fix"
	"github.com/tinovyatkin/tally/internal/processor"
	"github.com/tinovyatkin/tally/internal/reporter"
	"github.com/tinovyatkin/tally/internal/rules"
	_ "github.com/tinovyatkin/tally/internal/rules/all" // Register all rules
	"github.com/tinovyatkin/tally/internal/rules/buildkit"
	"github.com/tinovyatkin/tally/internal/rules/buildkit/fixes"
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/sourcemap"
	"github.com/tinovyatkin/tally/internal/version"
)

// Exit codes
const (
	ExitSuccess     = 0 // No violations (or below fail-level threshold)
	ExitViolations  = 1 // Violations found at or above fail-level
	ExitConfigError = 2 // Parse or config error
)

//nolint:gocyclo // CLI command with many flags inherently has high cyclomatic complexity
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
		Action: func(ctx stdcontext.Context, cmd *cli.Command) error {
			inputs := cmd.Args().Slice()

			if len(inputs) == 0 {
				// Default to current directory
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

			var allViolations []rules.Violation
			fileSources := make(map[string][]byte)
			fileConfigs := make(map[string]*config.Config) // Per-file configs
			var firstCfg *config.Config                    // Store first file's config for output settings

			for _, df := range discovered {
				file := df.Path

				// Load config for this specific file (cascading discovery)
				cfg, err := loadConfigForFile(cmd, file)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to load config for %s: %v\n", file, err)
					os.Exit(ExitConfigError)
				}

				// Store per-file config for processor chain
				fileConfigs[file] = cfg

				// Store first config for output settings
				if firstCfg == nil {
					firstCfg = cfg
				}

				// Parse the Dockerfile (pass config to optimize BuildKit's linter)
				parseResult, err := dockerfile.ParseFile(ctx, file, cfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to parse %s: %v\n", file, err)
					os.Exit(ExitConfigError)
				}

				// Store source for later use in text output
				fileSources[file] = parseResult.Source

				// Parse directives early to extract shell directives for the semantic model
				sm := sourcemap.New(parseResult.Source)
				directiveResult := directive.Parse(sm, nil)

				// Build semantic model for cross-instruction analysis
				// Note: buildArgs will be populated when --build-arg flag is implemented
				var buildArgs map[string]string
				sem := semantic.NewBuilder(parseResult, buildArgs, file).
					WithShellDirectives(directiveResult.ShellDirectives).
					Build()

				// Create build context if context directory is specified
				var buildCtx rules.BuildContext
				if df.ContextDir != "" {
					buildCtx, err = context.New(df.ContextDir, file,
						context.WithHeredocFiles(extractHeredocFiles(parseResult)))
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to create build context: %v\n", err)
						// Continue without context - rules will skip context-aware checks
					}
				}

				// Build base LintInput (without rule-specific config)
				baseInput := rules.LintInput{
					File:     file,
					AST:      parseResult.AST,
					Stages:   parseResult.Stages,
					MetaArgs: parseResult.MetaArgs,
					Source:   parseResult.Source,
					Semantic: sem,
					Context:  buildCtx,
				}

				// Collect construction-time violations from semantic analysis
				var violations []rules.Violation
				for _, issue := range sem.ConstructionIssues() {
					violations = append(violations, rules.NewViolation(
						rules.NewLocationFromRange(issue.File, issue.Location),
						issue.Code,
						issue.Message,
						rules.SeverityError,
					).WithDocURL(issue.DocURL))
				}
				for _, rule := range rules.All() {
					// Clone input and set rule-specific config
					input := baseInput
					input.Config = getRuleConfig(rule.Metadata().Code, cfg)
					violations = append(violations, rule.Check(input)...)
				}

				// Convert BuildKit warnings to violations
				for _, w := range parseResult.Warnings {
					violations = append(violations, rules.NewViolationFromBuildKitWarning(
						file,
						w.RuleName,
						w.Description,
						w.URL,
						w.Message,
						w.Location,
					))
				}

				// Enrich BuildKit violations with auto-fix suggestions
				fixes.EnrichBuildKitFixes(violations, sem, parseResult.Source)

				allViolations = append(allViolations, violations...)
			}

			// Build processor chain for violation processing
			// Order matters: severity first, then filter, then transform, then output preparation
			inlineFilter := processor.NewInlineDirectiveFilter()
			chain := processor.NewChain(
				processor.NewPathNormalization(),   // Normalize paths for cross-platform consistency
				processor.NewSeverityOverride(),    // Apply severity overrides (must run before EnableFilter)
				processor.NewEnableFilter(),        // Filter rules with severity="off"
				processor.NewPathExclusionFilter(), // Apply per-rule path exclusions
				inlineFilter,                       // Apply inline ignore directives
				processor.NewDeduplication(),       // Remove duplicate violations
				processor.NewSorting(),             // Stable output ordering
				processor.NewSnippetAttachment(),   // Attach source code snippets
			)

			// Process all violations through the chain
			// Each file gets its own config for rule enable/disable, severity, etc.
			procCtx := processor.NewContext(fileConfigs, firstCfg, fileSources)
			allViolations = chain.Process(allViolations, procCtx)

			// Add any additional violations from the inline directive filter
			// (parse errors, unused directives, missing reasons)
			additionalViolations := inlineFilter.AdditionalViolations()
			if len(additionalViolations) > 0 {
				// Apply PathNormalization for consistent path formats with main violations
				additionalViolations = processor.NewPathNormalization().Process(additionalViolations, procCtx)
				additionalViolations = processor.NewSnippetAttachment().Process(additionalViolations, procCtx)
				allViolations = append(allViolations, additionalViolations...)
				// Re-sort after adding directive warnings
				allViolations = reporter.SortViolations(allViolations)
			}

			// Apply fixes if --fix flag is set
			if cmd.Bool("fix-unsafe") && !cmd.Bool("fix") {
				fmt.Fprintf(os.Stderr, "Warning: --fix-unsafe has no effect without --fix\n")
			}
			if cmd.Bool("fix") {
				fixResult, fixErr := applyFixes(ctx, cmd, allViolations, fileSources, fileConfigs)
				if fixErr != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to apply fixes: %v\n", fixErr)
					return cli.Exit("", ExitConfigError)
				}

				// Report fix results
				if fixResult.TotalApplied() > 0 {
					fmt.Fprintf(os.Stderr, "Fixed %d issues in %d files\n",
						fixResult.TotalApplied(), fixResult.FilesModified())
				}
				if fixResult.TotalSkipped() > 0 {
					fmt.Fprintf(os.Stderr, "Skipped %d fixes\n", fixResult.TotalSkipped())
				}

				// Update violations to exclude fixed ones
				allViolations = filterFixedViolations(allViolations, fixResult)
			}

			// Get output configuration
			outCfg := getOutputConfig(cmd, firstCfg)

			// Parse format
			formatType, err := reporter.ParseFormat(outCfg.format)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return cli.Exit("", ExitConfigError)
			}

			// Get output writer
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

			// Build reporter options
			opts := reporter.Options{
				Format:      formatType,
				Writer:      writer,
				ShowSource:  outCfg.showSource,
				ToolName:    "tally",
				ToolVersion: version.Version(),
				ToolURI:     "https://github.com/tinovyatkin/tally",
			}

			// Handle color flag
			if cmd.IsSet("no-color") && cmd.Bool("no-color") {
				noColor := false
				opts.Color = &noColor
			}

			// Create reporter
			rep, err := reporter.New(opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to create reporter: %v\n", err)
				return cli.Exit("", ExitConfigError)
			}

			// Calculate metadata for report
			// Count rules that are effectively enabled based on config
			rulesEnabled := countEffectivelyEnabledRules(firstCfg)
			metadata := reporter.ReportMetadata{
				FilesScanned: len(discovered),
				RulesEnabled: rulesEnabled,
			}

			// Report violations
			if err := rep.Report(allViolations, fileSources, metadata); err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to write output: %v\n", err)
				return cli.Exit("", ExitConfigError)
			}

			// Determine exit code based on fail-level
			exitCode := determineExitCode(allViolations, outCfg.failLevel)
			if exitCode != ExitSuccess {
				return cli.Exit("", exitCode)
			}

			return nil
		},
	}
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

// getRuleConfig returns the appropriate config for a rule based on its code.
// This allows each rule to receive its own typed config from the global config.
func getRuleConfig(ruleCode string, cfg *config.Config) any {
	// Return the rule's options map from config
	// The rule's resolveConfig method handles converting map to typed config
	return cfg.Rules.GetOptions(ruleCode)
}

// countEffectivelyEnabledRules returns the number of rules that are actually enabled
// after applying config overrides (include/exclude patterns and severity overrides).
// Counts both registered rules and BuildKit parser rules.
func countEffectivelyEnabledRules(cfg *config.Config) int {
	count := 0

	// Count registered rules (tally/*, hadolint/*, and implemented buildkit/* rules)
	registry := rules.DefaultRegistry()
	for _, rule := range registry.All() {
		if isRuleEnabled(rule.Metadata().Code, rule.Metadata().DefaultSeverity, cfg) {
			count++
		}
	}

	// Count BuildKit parser rules (captured warnings like StageNameCasing, etc.)
	// These are always enabled unless explicitly disabled via config
	for _, info := range buildkit.All() {
		ruleCode := rules.BuildKitRulePrefix + info.Name
		if isRuleEnabled(ruleCode, info.DefaultSeverity, cfg) {
			count++
		}
	}

	return count
}

// isRuleEnabled checks if a rule is effectively enabled based on config.
func isRuleEnabled(ruleCode string, defaultSeverity rules.Severity, cfg *config.Config) bool {
	if cfg == nil {
		return defaultSeverity != rules.SeverityOff
	}

	// Check if explicitly disabled by exclude pattern
	enabled := cfg.Rules.IsEnabled(ruleCode)
	if enabled != nil {
		return *enabled
	}

	// Check if severity is overridden to "off"
	if sev := cfg.Rules.GetSeverity(ruleCode); sev == "off" {
		return false
	}

	// Check if "off" rule is auto-enabled by having config options
	if defaultSeverity == rules.SeverityOff {
		ruleConfig := cfg.Rules.Get(ruleCode)
		return ruleConfig != nil && len(ruleConfig.Options) > 0
	}

	// Use default severity
	return defaultSeverity != rules.SeverityOff
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
		modes := buildFixModes(cfg)
		if len(modes) > 0 {
			result[filePath] = modes
		}
	}
	return result
}

// buildFixModes extracts fix mode configuration for all rules from a single config.
func buildFixModes(cfg *config.Config) map[string]fix.FixMode {
	modes := make(map[string]fix.FixMode)

	// Helper to add modes from a namespace map
	addFromNamespace := func(namespace string, ruleConfigs map[string]config.RuleConfig) {
		for name, ruleCfg := range ruleConfigs {
			if ruleCfg.Fix != "" {
				ruleCode := namespace + "/" + name
				modes[ruleCode] = ruleCfg.Fix
			}
		}
	}

	// Process each namespace
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
