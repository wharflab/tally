package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/directive"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/reporter"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/maxlines"
	_ "github.com/tinovyatkin/tally/internal/rules/nounreachablestages" // Register rule
	"github.com/tinovyatkin/tally/internal/semantic"
	"github.com/tinovyatkin/tally/internal/sourcemap"
)

// FileResult contains the linting results for a single file.
type FileResult struct {
	File       string            `json:"file"`
	Violations []rules.Violation `json:"violations"`
}

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
				Usage:   "Output format: text, json",
				Sources: cli.EnvVars("TALLY_FORMAT"),
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
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			files := cmd.Args().Slice()

			if len(files) == 0 {
				// Default to Dockerfile in current directory
				files = []string{"Dockerfile"}
			}

			var allResults []FileResult
			hasViolations := false
			var configFormat string // Store format from first file's config
			fileSources := make(map[string][]byte)

			for _, file := range files {
				// Load config for this specific file (cascading discovery)
				cfg, err := loadConfigForFile(cmd, file)
				if err != nil {
					return fmt.Errorf("failed to load config for %s: %w", file, err)
				}

				// Store format from first file's config (used if CLI flag not set)
				if configFormat == "" {
					configFormat = cfg.Format
				}

				// Parse the Dockerfile
				parseResult, err := dockerfile.ParseFile(ctx, file)
				if err != nil {
					return fmt.Errorf("failed to parse %s: %w", file, err)
				}

				// Store source for later use in text output
				fileSources[file] = parseResult.Source

				// Build semantic model for cross-instruction analysis
				// Note: buildArgs will be populated when --build-arg flag is implemented
				var buildArgs map[string]string
				sem := semantic.NewModel(parseResult, buildArgs, file)

				// Build base LintInput (without rule-specific config)
				baseInput := rules.LintInput{
					File:     file,
					AST:      parseResult.AST,
					Stages:   parseResult.Stages,
					MetaArgs: parseResult.MetaArgs,
					Source:   parseResult.Source,
					Semantic: sem,
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

				// Parse inline directives
				sm := sourcemap.New(parseResult.Source)
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
				if cfg.InlineDirectives.Enabled && len(directiveResult.Directives) > 0 {
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

				if len(violations) > 0 {
					hasViolations = true
				}

				allResults = append(allResults, FileResult{
					File:       file,
					Violations: violations,
				})
			}

			// Output results
			format := getFormat(cmd, configFormat)
			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(allResults); err != nil {
					return fmt.Errorf("failed to encode JSON: %w", err)
				}
			default:
				// Collect all violations for the reporter
				var allViolations []rules.Violation
				for _, result := range allResults {
					allViolations = append(allViolations, result.Violations...)
				}
				if err := reporter.PrintText(os.Stdout, allViolations, fileSources); err != nil {
					return fmt.Errorf("failed to print results: %w", err)
				}
			}

			// Exit with error code if violations found (consistent for all formats)
			if hasViolations {
				os.Exit(1)
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

	if cmd.IsSet("format") {
		cfg.Format = cmd.String("format")
	}

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

// getFormat determines the output format.
// Uses CLI flag if set, otherwise falls back to the provided config format.
func getFormat(cmd *cli.Command, configFormat string) string {
	// CLI flag takes precedence
	if cmd.IsSet("format") {
		return cmd.String("format")
	}

	// Use format from config if set
	if configFormat != "" {
		return configFormat
	}

	return "text"
}

// getRuleConfig returns the appropriate config for a rule based on its code.
// This allows each rule to receive its own typed config from the global config.
func getRuleConfig(ruleCode string, cfg *config.Config) any {
	switch ruleCode {
	case "max-lines":
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
