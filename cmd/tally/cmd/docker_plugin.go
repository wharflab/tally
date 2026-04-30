package cmd

import (
	"errors"
	"path"
	"strings"

	dockercli "github.com/docker/cli/cli"
	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/docker/cli/cli-plugins/plugin"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/debug"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"

	"github.com/wharflab/tally/internal/version"
)

const dockerLintPluginName = "lint"

// IsDockerLintPluginExecutable reports whether executable should run tally in
// Docker CLI plugin mode.
func IsDockerLintPluginExecutable(executable string) bool {
	base := strings.ToLower(path.Base(strings.ReplaceAll(executable, "\\", "/")))
	base = strings.TrimSuffix(base, ".exe")
	return base == metadata.NamePrefix+dockerLintPluginName
}

// ExecuteDockerPlugin runs tally as the docker-lint CLI plugin.
func ExecuteDockerPlugin() error {
	otel.SetErrorHandler(debug.OTELErrorHandler)

	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return err
	}
	return plugin.RunPlugin(dockerCLI, newDockerLintPluginCommand(dockerCLI), dockerPluginMetadata())
}

func newDockerLintPluginCommand(dockerCLI command.Cli) *cobra.Command {
	opts := &lintOptions{}
	cmd := newLintCommand(opts)
	cmd.Version = version.Version()
	cmd.SetVersionTemplate("docker-lint version {{.Version}}\n")
	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := plugin.PersistentPreRunE(cmd, args); err != nil {
			return err
		}
		opts.dockerPlugin = dockerPluginContextFrom(dockerCLI)
		return nil
	}
	return cmd
}

func dockerPluginMetadata() metadata.Metadata {
	return metadata.Metadata{
		SchemaVersion:    "0.1.0",
		Vendor:           "Wharflab",
		Version:          version.Version(),
		ShortDescription: "Lint Dockerfiles and Containerfiles",
		URL:              "https://tally.wharflab.com/",
	}
}

func dockerPluginContextFrom(dockerCLI command.Cli) *dockerPluginContext {
	if dockerCLI == nil {
		return nil
	}

	ctx := &dockerPluginContext{
		CurrentContext: dockerCLI.CurrentContext(),
	}
	if cfg := dockerCLI.ConfigFile(); cfg != nil {
		ctx.ConfigPath = cfg.GetFilename()
	}
	if ep := dockerCLI.DockerEndpoint(); ep.Host != "" {
		ctx.EndpointHost = ep.Host
	}
	return ctx
}

// ExitStatus maps command-layer typed errors to process exit behavior. The
// optional message is for Docker CLI status errors; ExitError messages are
// already written by command handlers before returning.
func ExitStatus(err error) (code int, message string, ok bool) {
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code, "", true
	}

	var statusErr dockercli.StatusError
	if errors.As(err, &statusErr) {
		code := statusErr.StatusCode
		if code == 0 {
			code = 1
		}
		return code, statusErr.Error(), true
	}

	return 0, "", false
}
