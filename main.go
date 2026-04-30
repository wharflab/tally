package main

import (
	"fmt"
	"os"

	"github.com/wharflab/tally/cmd/tally/cmd"
)

func main() {
	if err := cmd.ExecuteForExecutable(os.Args[0]); err != nil {
		if code, message, ok := cmd.ExitStatus(err); ok {
			if message != "" {
				fmt.Fprintln(os.Stderr, message)
			}
			os.Exit(code)
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
