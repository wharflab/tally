package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/wharflab/tally/internal/lspserver"
)

func lspCommand() *cobra.Command {
	var stdio bool

	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Start the Language Server Protocol server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !stdio {
				fmt.Fprintln(os.Stderr, "Error: only --stdio transport is supported")
				return exitWith(ExitConfigError)
			}
			server := lspserver.New()
			return server.RunStdio(cmd.Context())
		},
	}

	cmd.Flags().BoolVar(&stdio, "stdio", true, "Use stdin/stdout for communication (required)")
	return cmd
}
