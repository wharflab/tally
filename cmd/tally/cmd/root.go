package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wharflab/tally/internal/version"
)

// ExitError carries a typed exit code through Cobra's error path.
// Execute's caller (main) inspects this to set the process exit code without
// printing the Cobra default "Error:" prefix.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("exit %d", e.Code)
}

// exitWith returns an ExitError with no message. The caller is expected to
// have already written any user-facing error output to stderr.
func exitWith(code int) error {
	return &ExitError{Code: code}
}

// NewRootCommand creates the tally Cobra root command.
func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tally",
		Short:   "A linter for Dockerfiles and Containerfiles",
		Version: version.Version(),
		Long: `tally is a fast, configurable linter for Dockerfiles and Containerfiles.

It checks your container build files for best practices, security issues,
and common mistakes.

Examples:
  tally lint Dockerfile
  tally lint --max-lines 100 Dockerfile
  tally lint .`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.SetVersionTemplate("tally version {{.Version}}\n")

	cmd.AddCommand(lintCommand())
	cmd.AddCommand(lspCommand())
	cmd.AddCommand(versionCommand())

	return cmd
}

// Execute runs the CLI application.
func Execute() error {
	return NewRootCommand().Execute()
}

// ExecuteForExecutable dispatches to the standalone CLI or Docker CLI plugin
// mode based on the invoked executable name.
func ExecuteForExecutable(executable string) error {
	if IsDockerLintPluginExecutable(executable) {
		return ExecuteDockerPlugin()
	}
	return Execute()
}
