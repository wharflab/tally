package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/rules"
	"github.com/tinovyatkin/tally/internal/rules/maxlines"
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
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			files := cmd.Args().Slice()

			if len(files) == 0 {
				// Default to Dockerfile in current directory
				files = []string{"Dockerfile"}
			}

			var allResults []FileResult
			hasViolations := false

			for _, file := range files {
				// Load config for this specific file (cascading discovery)
				cfg, err := loadConfigForFile(cmd, file)
				if err != nil {
					return fmt.Errorf("failed to load config for %s: %w", file, err)
				}

				// Parse the Dockerfile
				parseResult, err := dockerfile.ParseFile(ctx, file)
				if err != nil {
					return fmt.Errorf("failed to parse %s: %w", file, err)
				}

				// Build LintInput for rules
				input := rules.LintInput{
					File:     file,
					AST:      parseResult.AST,
					Stages:   parseResult.Stages,
					MetaArgs: parseResult.MetaArgs,
					Source:   parseResult.Source,
					Config: maxlines.Config{
						Max:            cfg.Rules.MaxLines.Max,
						SkipBlankLines: cfg.Rules.MaxLines.SkipBlankLines,
						SkipComments:   cfg.Rules.MaxLines.SkipComments,
					},
				}

				// Run all registered rules
				var violations []rules.Violation
				for _, rule := range rules.All() {
					violations = append(violations, rule.Check(input)...)
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
			format := getFormat(cmd, allResults)
			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(allResults); err != nil {
					return fmt.Errorf("failed to encode JSON: %w", err)
				}
			default:
				for _, result := range allResults {
					for _, v := range result.Violations {
						line := v.Line() + 1 // Convert 0-based to 1-based for display
						fmt.Printf("%s:%d: %s (%s)\n", result.File, line, v.Message, v.RuleCode)
					}
				}
				if hasViolations {
					os.Exit(1)
				}
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

	return cfg, nil
}

// getFormat determines the output format.
func getFormat(cmd *cli.Command, results []FileResult) string {
	// CLI flag takes precedence
	if cmd.IsSet("format") {
		return cmd.String("format")
	}

	// Otherwise use the format from the first result's config
	if len(results) > 0 {
		cfg, err := config.Load(results[0].File)
		if err == nil {
			return cfg.Format
		}
	}

	return "text"
}
