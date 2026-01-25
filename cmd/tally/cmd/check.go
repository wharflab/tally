package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/tally/internal/config"
	"github.com/tinovyatkin/tally/internal/dockerfile"
	"github.com/tinovyatkin/tally/internal/lint"
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
				Name:  "skip-blank-lines",
				Usage: "Exclude blank lines from the line count",
			},
			&cli.BoolFlag{
				Name:  "skip-comments",
				Usage: "Exclude comment lines from the line count",
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

			var allResults []lint.FileResult
			hasIssues := false

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

				// Run linting rules
				var issues []lint.Issue

				// Check max-lines rule
				if issue := lint.CheckMaxLines(parseResult, cfg.Rules.MaxLines); issue != nil {
					issues = append(issues, *issue)
					hasIssues = true
				}

				allResults = append(allResults, lint.FileResult{
					File:   file,
					Lines:  parseResult.TotalLines,
					Issues: issues,
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
					for _, issue := range result.Issues {
						fmt.Printf("%s:%d: %s (%s)\n", result.File, issue.Line, issue.Message, issue.Rule)
					}
				}
				if hasIssues {
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

// getFormat determines the output format, using the first file's config as reference.
func getFormat(cmd *cli.Command, results []lint.FileResult) string {
	// CLI flag takes precedence
	if cmd.IsSet("format") {
		return cmd.String("format")
	}

	// Otherwise use the format from the first result's config
	// (This is a simplification - in practice all files might have different configs)
	if len(results) > 0 {
		// Load config for first file to get format
		cfg, err := config.Load(results[0].File)
		if err == nil {
			return cfg.Format
		}
	}

	return "text"
}
