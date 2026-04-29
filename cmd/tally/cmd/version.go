package cmd

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/wharflab/tally/internal/shellcheck"
	"github.com/wharflab/tally/internal/version"
)

func versionCommand() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if asJSON {
				info := version.GetInfo()
				runner := shellcheck.NewRunner()
				defer runner.Close(ctx)
				if v, err := runner.Version(ctx); err == nil {
					info.ShellcheckVersion = v
				}
				return json.MarshalWrite(
					os.Stdout,
					info,
					jsontext.EscapeForHTML(true),
					jsontext.WithIndentPrefix(""),
					jsontext.WithIndent("  "),
				)
			}
			fmt.Printf("tally version %s\n", version.Version())
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output version information as JSON")
	return cmd
}
