package cmd

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/tinovyatkin/tally/internal/version"
)

// NewApp creates the CLI application
func NewApp() *cli.Command {
	return &cli.Command{
		Name:    "tally",
		Usage:   "A linter for Dockerfiles and Containerfiles",
		Version: version.Version(),
		Description: `tally is a fast, configurable linter for Dockerfiles and Containerfiles.

It checks your container build files for best practices, security issues,
and common mistakes.

Examples:
  tally lint Dockerfile
  tally lint --max-lines 100 Dockerfile
  tally lint .`,
		Commands: []*cli.Command{
			lintCommand(),
			lspCommand(),
			versionCommand(),
		},
	}
}

// Execute runs the CLI application
func Execute() error {
	return NewApp().Run(context.Background(), os.Args)
}
