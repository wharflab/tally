package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/wharflab/tally/internal/lspserver"
)

func lspCommand() *cli.Command {
	return &cli.Command{
		Name:  "lsp",
		Usage: "Start the Language Server Protocol server",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "stdio",
				Usage: "Use stdin/stdout for communication (required)",
				Value: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if !cmd.Bool("stdio") {
				fmt.Fprintln(os.Stderr, "Error: only --stdio transport is supported")
				return cli.Exit("", ExitConfigError)
			}

			server := lspserver.New()
			return server.RunStdio(ctx)
		},
	}
}
