package cmd

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/wharflab/tally/internal/version"
)

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Print version information",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "json",
				Usage: "Output version information as JSON",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Bool("json") {
				return json.MarshalWrite(
					os.Stdout,
					version.GetInfo(),
					jsontext.EscapeForHTML(true),
					jsontext.WithIndentPrefix(""),
					jsontext.WithIndent("  "),
				)
			}
			fmt.Printf("tally version %s\n", version.Version())
			return nil
		},
	}
}
