package cmd

import (
	stdcontext "context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/context"
	"github.com/tinovyatkin/tally/internal/directive"
	"github.com/tinovyatkin/tally/internal/discovery"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/reporter"
	"github.com/tinovyatkin/tally/internal/rules"
	_ "github.com/tinovyatkin/tally/internal/rules/all" // Register all rules
	"github.com/tinovyatkin/tally/internal/rules/maxlines"
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
			&cli.StringFlag{
				Name:    "context",
				Usage:   "Build context directory for context-aware rules",
				Sources: cli.EnvVars("TALLY_CONTEXT"),
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
			var firstCfg *config.Config // Store first file's config for output settings

			for _, df := range discovered {
				file := df.Path

				// Load config for this specific file (cascading discovery)
				cfg, err := loadConfigForFile(cmd, file)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to load config for %s: %v\n", file, err)
					os.Exit(ExitConfigError)
				}

				// Store first config for output settings
				if firstCfg == nil {
					firstCfg = cfg
				}

				// Parse the Dockerfile
				parseResult, err := dockerfile.ParseFile(ctx, file)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: failed to parse %s: %v\n", file, err)
					os.Exit(ExitConfigError)
				}

				// Store source for later use in text output
				fileSources[file] = parseResult.Source

				// Build semantic model for cross-instruction analysis
				// Note: buildArgs will be populated when --build-arg flag is implemented
				var buildArgs map[string]string
				sem := semantic.NewModel(parseResult, buildArgs, file)

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

				// Apply inline directives if enabled
				violations = processInlineDirectives(file, parseResult.Source, violations, cfg)

				allViolations = append(allViolations, violations...)
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

			// Report violations
			if err := rep.Report(allViolations, fileSources); err != nil {
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

	// Apply CLI flag overrides (highest priority)
	// Only override if the flag was explicitly set
	if cmd.IsSet("max-lines") {
		cfg.Rules.MaxLines.Max = cmd.Int("max-lines")
	}

	if cmd.IsSet("skip-blank-lines") {
		cfg.Rules.MaxLines.SkipBlankLines = cmd.Bool("skip-blank-lines")
	}

	if cmd.IsSet("skip-comments") {
		cfg.Rules.MaxLines.SkipComments = cmd.Bool("skip-comments")
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

// processInlineDirectives handles parsing and applying inline ignore directives.
// It filters violations based on directives and reports directive-related warnings.
func processInlineDirectives(
	file string,
	source []byte,
	violations []rules.Violation,
	cfg *config.Config,
) []rules.Violation {
	if !cfg.InlineDirectives.Enabled {
		return violations
	}

	sm := sourcemap.New(source)
	var validator directive.RuleValidator
	if cfg.InlineDirectives.ValidateRules {
		validator = rules.DefaultRegistry().Has
	}
	directiveResult := directive.Parse(sm, validator)

	// Report parse errors as warnings
	for _, parseErr := range directiveResult.Errors {
		violations = append(violations, rules.NewViolation(
			rules.NewLineLocation(file, parseErr.Line+1),
			"invalid-ignore-directive",
			parseErr.Message,
			rules.SeverityWarning,
		).WithDetail("Directive: "+parseErr.RawText))
	}

	// Filter violations based on inline directives
	if len(directiveResult.Directives) > 0 {
		filterResult := directive.Filter(violations, directiveResult.Directives)
		violations = filterResult.Violations

		// Report unused directives if configured
		if cfg.InlineDirectives.WarnUnused {
			for _, unused := range filterResult.UnusedDirectives {
				violations = append(violations, rules.NewViolation(
					rules.NewLineLocation(file, unused.Line+1),
					"unused-ignore-directive",
					"ignore directive does not suppress any violations",
					rules.SeverityWarning,
				).WithDetail("Directive: "+unused.RawText))
			}
		}
	}

	// Report directives without reason if configured
	// Only applies to tally and hadolint sources (buildx doesn't support reason=)
	if cfg.InlineDirectives.RequireReason {
		for _, d := range directiveResult.Directives {
			if d.Source != directive.SourceBuildx && d.Reason == "" {
				violations = append(violations, rules.NewViolation(
					rules.NewLineLocation(file, d.Line+1),
					"missing-directive-reason",
					"ignore directive is missing reason= explanation",
					rules.SeverityWarning,
				).WithDetail("Directive: "+d.RawText))
			}
		}
	}

	return violations
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
	switch ruleCode {
	case rules.TallyRulePrefix + "max-lines":
		return maxlines.Config{
			Max:            cfg.Rules.MaxLines.Max,
			SkipBlankLines: cfg.Rules.MaxLines.SkipBlankLines,
			SkipComments:   cfg.Rules.MaxLines.SkipComments,
		}
	default:
		// Unknown rules get nil config (use their defaults)
		return nil
	}
}

// extractHeredocFiles extracts virtual file paths from heredoc COPY/ADD commands.
// These are inline files created by heredoc syntax that should not be checked
// against .dockerignore.
func extractHeredocFiles(parseResult *dockerfile.ParseResult) map[string]bool {
	return dockerfile.ExtractHeredocFiles(parseResult.Stages)
}
