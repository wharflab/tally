# Docker CLI Plugin Support

**Status:** Implementation plan

**Last researched:** 2026-04-29

**Primary goal:** make tally usable as a Docker CLI plugin so users can run Dockerfile linting as `docker lint ...`, while preserving the
standalone `tally` CLI and aligning Homebrew packaging with Docker Buildx and Compose plugin formula conventions.

**Related design docs:**

- [02 - Docker Buildx Bake `--check` Analysis](02-buildx-bake-check-analysis.md)
- [05 - Reporters and Output Formatting](05-reporters-and-output.md)
- [38 - BuildInvocation, Orchestrator Entrypoints, and IDE Integration](38-buildinvocation-bake.md)

---

## Decision Summary

Ship Docker CLI plugin support through two implementation phases:

1. migrate the standalone CLI from `urfave/cli/v3` to Cobra with no intended user-facing behavior changes
2. add `docker lint` using Docker's Cobra-based CLI plugin helper

The target user-facing result is:

```sh
tally lint Dockerfile
docker lint Dockerfile
```

The plugin command is exposed through the executable name `docker-lint`. Docker runs plugin executables with the original Docker argv after
`docker`, so `docker lint Dockerfile` reaches the executable as `docker-lint lint Dockerfile`. Docker's `plugin.Run` helper supplies the outer
dummy Cobra root and mounts tally's `lint` command beneath it; the implementation should not treat the leading `lint` token as a Dockerfile path.

For Homebrew, follow the current Homebrew core pattern used by `docker-buildx` and `docker-compose`:

- install the normal `tally` executable into `bin`
- install a symlink named `docker-lint` under `#{HOMEBREW_PREFIX}/lib/docker/cli-plugins`
- add a caveat instructing users to add that directory to Docker CLI `cliPluginsExtraDirs`
- do not write to `~/.docker/config.json` during install

The implementation must not use Docker Engine plugins. Docker Engine plugins are a separate daemon-side plugin model and are unrelated to this
feature.

---

## Research Notes

Docker CLI plugins are ordinary executables discovered by the Docker CLI. The executable name must begin with `docker-`, and every plugin must
handle the `docker-cli-plugin-metadata` subcommand by printing metadata JSON.

Relevant upstream constants are in Docker CLI's metadata package:

- `NamePrefix = "docker-"`
- `MetadataSubcommandName = "docker-cli-plugin-metadata"`
- metadata schema version is `"0.1.0"`

Reference: <https://pkg.go.dev/github.com/docker/cli/cli-plugins/metadata>

Docker also provides a Cobra plugin helper in `github.com/docker/cli/cli-plugins/plugin`.

The helper's value proposition:

- wraps a plugin command in Docker's top-level command machinery
- adds Docker global flags such as `--config`, `--context`, `--host`, TLS flags, debug, and log-level
- parses Docker global flags before the plugin command without consuming plugin-local flags after `lint`
- initializes Docker CLI config, context store, streams, and optional API client access
- exposes the hidden `docker-cli-plugin-metadata` command
- wires Docker-style help, completion, flag errors, status errors, and parent CLI cancellation through `DOCKER_CLI_PLUGIN_SOCKET`
- accounts for Docker's plugin argv shape: the Docker CLI manager runs `docker-lint` with `os.Args[1:]`, so the leading `lint` command name is
  still present when the plugin process starts

The helper is not lightweight: `go list -deps github.com/docker/cli/cli-plugins/plugin` currently reports hundreds of packages including Docker
CLI command machinery, Moby client types, OpenTelemetry, and gRPC. This repo already depends on much of Docker/Buildx, and the benefit of correct
Docker global flag and current-context handling outweighs the dependency cost for this feature.

Reference: <https://pkg.go.dev/github.com/docker/cli/cli-plugins/plugin>

Koanf also has a pflag provider in `github.com/knadh/koanf/providers/posflag`, which is directly relevant because Cobra uses pflag. This is a
useful bridge for Phase 1:

- it loads pflag values into koanf maps, so simple config-shaped Cobra flags can participate in the same defaults -> config file -> env -> CLI
  layering as the rest of `internal/config`
- it tracks pflag's `Changed` state and, when given a live koanf instance, avoids letting unchanged flag defaults overwrite values already loaded
  from config files or environment variables
- `ProviderWithFlag` allows tally to map user-facing flag names to canonical config keys and use `posflag.FlagVal` for typed values
- returning an empty key from the callback lets tally deliberately exclude operational flags from koanf

This does not remove the need for a small `lintOptions` layer. Several flags are not plain config keys:

- `--hide-source` and `--no-inline-directives` invert config values
- `--select` / `--ignore` currently append to configured rule selections instead of replacing them
- `--acp-command` parses a shell-like command string into `ai.command` and also enables AI; because tally already depends on
  `mvdan.cc/sh/v3`, the migration should prefer that parser or a shared internal shell helper over extending a hand-rolled splitter
- `--fix`, `--fix-rule`, `--fix-unsafe`, `--target`, `--service`, `--context`, and `NO_COLOR` are invocation behavior rather than persistent
  configuration

Recommended Phase 1 shape: use `posflag.ProviderWithFlag` for simple config-shaped flags, then apply complex/operational flags through
`lintOptions`. The standard-library `basicflag` provider is not useful for the migrated CLI because Cobra does not use the standard `flag`
package.

Reference: <https://pkg.go.dev/github.com/knadh/koanf/providers/posflag>

Homebrew core formula examples:

- `docker-buildx` installs `(lib/"docker/cli-plugins").install_symlink bin/"docker-buildx"` and tells users to configure
  `cliPluginsExtraDirs`.
  Reference: <https://github.com/Homebrew/homebrew-core/blob/main/Formula/d/docker-buildx.rb>
- `docker-compose` uses the same plugin directory and caveat pattern.
  Reference: <https://github.com/Homebrew/homebrew-core/blob/main/Formula/d/docker-compose.rb>
- `buildkit` is not a Docker CLI plugin formula; it installs `buildctl`.
  Reference: <https://github.com/Homebrew/homebrew-core/blob/main/Formula/b/buildkit.rb>

Docker Bake does not have a separate Homebrew formula. It is exposed as `docker buildx bake`.

Current tally release packaging already covers:

- GitHub release archives and direct Windows `.exe` assets
- GHCR container images
- Homebrew tap formula
- NPM, PyPI, and RubyGems packages
- WinGet manifests
- editor-extension marketplaces from the release workflow

For CLI plugin packaging, only Homebrew and WinGet need package-manager-specific decisions in the MVP. The other package managers can document a
manual plugin registration step.

WinGet precedent is different from Homebrew:

- `Docker.Buildx` is a WinGet portable package with `Commands: docker-buildx`; it does not install into Docker's CLI plugin directory.
  Reference: <https://github.com/microsoft/winget-pkgs/blob/master/manifests/d/Docker/Buildx/0.32.1/Docker.Buildx.installer.yaml>
- `Docker.DockerCompose` is also a WinGet portable package with `Commands: docker-compose`; it likewise does not install into Docker's CLI plugin
  directory.
  Reference: <https://github.com/microsoft/winget-pkgs/blob/master/manifests/d/Docker/DockerCompose/5.1.3/Docker.DockerCompose.installer.yaml>
- The WinGet portable installer validator currently accepts only zero or one `Commands` value per portable installer. Keep `tally` as that command;
  `docker-lint` must be registered later with `tally register-docker-plugin`.

Docker CLI plugin discovery looks for `docker-*` executables in the Docker config `cli-plugins` directory, configured `cliPluginsExtraDirs`, and
platform system plugin directories. On Windows, the system plugin directory is `%ProgramFiles%\Docker\cli-plugins`; user installs should prefer
`%USERPROFILE%\.docker\cli-plugins`.
Reference: <https://github.com/docker/cli/blob/master/cli-plugins/manager/manager.go>

There is no Docker CLI plugin registration mechanism through the Windows registry in the current Docker CLI implementation. The Windows-specific
plugin manager file defines only the default system directory, and the shared plugin manager enumerates directories with `os.ReadDir`, checks for
regular files or symlinks named `docker-*`, then executes the selected file by absolute path. It does not search `PATH`, App Paths, or registry
keys for plugins.
Reference: <https://github.com/docker/cli/blob/master/cli-plugins/manager/manager_windows.go>

---

## User-Facing Behavior

### Standalone CLI

Existing commands continue to work:

```sh
tally lint Dockerfile
tally lint .
tally lint docker-bake.hcl --target web
tally version
tally lsp --stdio
```

No standalone command is removed or renamed.

### Docker CLI Plugin

The plugin UX is intentionally lint-focused:

```sh
docker lint Dockerfile
docker lint .
docker lint --format json Dockerfile
docker lint --fix Dockerfile
docker lint docker-bake.hcl --target web
docker lint compose.yaml --service api
```

This invocation maps to `tally lint`, not to the whole `tally` command tree. The following should not be supported as Docker plugin commands in
the MVP:

```sh
docker lint lsp --stdio
docker lint version
```

Rationale: `docker lint <path>` should treat positional values as lint targets. Making `version` or `lsp` special would create ambiguity with
real files named `version` or `lsp`.

Plugin version information should be available through Docker's metadata probe and through a Cobra root-level version flag if it stays unambiguous:

```sh
docker-lint docker-cli-plugin-metadata
docker lint --version
```

### Command Name Choice

MVP target: `docker lint`.

Known risk: `lint` is a generic Docker plugin command name. If another `docker-lint` plugin or a future Docker built-in command exists, users may
see a conflict or a different command may win discovery precedence.

Mitigation:

- keep the normal `tally` CLI as the authoritative interface
- document how to inspect installed plugins and remove conflicting `docker-lint` binaries
- do not ship a second `docker-tally` plugin unless the project decides the safer, namespaced command is worth the extra surface area

---

## Phase 1: Cobra Migration

Phase 1 changes only the standalone tally CLI framework. It must not add Docker plugin behavior yet.

### Current CLI Shape

The current `urfave/cli/v3` usage is concentrated in:

- `cmd/tally/cmd/root.go`
- `cmd/tally/cmd/lint.go`
- `cmd/tally/cmd/lsp.go`
- `cmd/tally/cmd/version.go`

The command tree is small:

- `tally lint [DOCKERFILE...]`
- `tally lsp --stdio`
- `tally version [--json]`

The migration surface is mostly the `lint` command's flags and the way command handlers query flag values.

Current `urfave/cli/v3` features used:

- string, int, bool, and string-slice flags
- aliases such as `--config, -c`, `--max-lines, -l`, `--format, -f`, `--output, -o`
- env-backed flag sources through `Sources: cli.EnvVars(...)`
- `cmd.IsSet(...)` to distinguish explicit input from defaults
- `cmd.Args().Slice()` for positional lint inputs
- a validator on `--format`
- `cli.Exit("", code)` for typed exit codes
- bool defaults set to `true` for `--show-source` and `--ai-redact-secrets`

This is straightforward to represent with Cobra and pflag. The risky part is preserving `IsSet` and env-source semantics.

### Target Cobra Shape

Suggested files:

- `cmd/tally/cmd/root.go`: Cobra root command and `Execute`
- `cmd/tally/cmd/lint.go`: Cobra lint command, or split flag/options helpers into a companion file
- `cmd/tally/cmd/lsp.go`: Cobra LSP command
- `cmd/tally/cmd/version.go`: Cobra version command
- `cmd/tally/cmd/options.go`: optional shared CLI options types

Recommended pattern:

```go
type lintOptions struct {
    configPath     string
    maxLines       *int
    skipBlankLines *bool
    // ...
}

func newLintCommand() *cobra.Command {
    opts := &lintOptions{}
    cmd := &cobra.Command{
        Use:   "lint [DOCKERFILE...]",
        Short: "Lint Dockerfile(s) for issues",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runLint(cmd.Context(), opts, args)
        },
    }
    addLintFlags(cmd, opts)
    return cmd
}
```

Prefer passing `lintOptions` through the lint pipeline instead of passing `*cobra.Command` deeply. That gives two benefits:

- the linter no longer depends on a specific CLI framework
- Docker plugin phase can reuse the same options parser while adding Docker CLI context data

For option values where "unset" and "explicitly set to the default" differ, use pointer fields (`*bool`, `*int`, `*string`) instead of parallel
`Foo` / `FooSet` fields. Keep plain values for fields where absence has no behavioral meaning.

The minimum viable refactor is to replace `*cli.Command` with a small interface or `lintOptions` on these internal helpers:

- `runLint`
- `runLintStdin`
- `loadConfigForFile`
- `getOutputConfig`
- `applyFixes`
- orchestrator discovery helpers that read `--target` / `--service`

### Koanf And Env Handling

The koanf configuration loader does not depend on `urfave/cli/v3`. It already accepts:

- filesystem config through `config.Load` / `config.LoadFromFile`
- `TALLY_*` env vars through `internal/config`
- editor/LSP overrides through `config.LoadWithOverrides`

The migration can improve config layering by making the split explicit:

1. **Config-shaped env vars** remain owned by koanf.
2. **Simple config-shaped command-line flags** are loaded into koanf through `posflag.ProviderWithFlag` after defaults, config file, and env.
3. **CLI-only env aliases** are handled by a small `applyEnvToLintOptions` helper, not by koanf.
4. **Complex command-line flags** are applied after koanf decode through `lintOptions`.

This matters because `urfave/cli/v3` currently treats env-backed flags as `IsSet`. Cobra/pflag does not have built-in env sources; `Flag.Changed`
only tracks command-line input. If we simply replace `cmd.IsSet` with `cmd.Flags().Changed`, several current env aliases would stop working.
Koanf's `posflag` provider preserves the important precedence behavior for command-line flags: changed pflags always override, while unchanged
defaults only fill a key if no earlier provider produced a value.

Known env categories:

| Category | Examples | Phase 1 handling |
|---|---|---|
| Config-shaped env vars | `TALLY_OUTPUT_FORMAT`, `TALLY_OUTPUT_SHOW_SOURCE`, `TALLY_RULES_MAX_LINES_MAX`, `TALLY_AI_TIMEOUT` | keep in koanf |
| Compatibility output aliases | `TALLY_FORMAT`, `TALLY_OUTPUT_FORMAT` | keep in koanf alias normalization |
| CLI-only env aliases | `NO_COLOR`, `TALLY_CONTEXT`, `TALLY_EXCLUDE`, `TALLY_RULES_SELECT`, `TALLY_RULES_IGNORE`, `TALLY_NO_INLINE_DIRECTIVES`, `TALLY_SLOW_CHECKS`, `TALLY_FIX`, `TALLY_FIX_RULE`, `TALLY_FIX_UNSAFE` | load into `lintOptions` explicitly |
| Orchestrator selector env vars | none today for `--target` / `--service` | no change |

Recommended CLI flag categories:

| Category | Examples | Phase 1 handling |
|---|---|---|
| Direct config keys | `--format`, `--output`, `--show-source`, `--fail-level`, `--warn-unused-directives`, `--require-reason`, `--slow-checks`, `--slow-checks-timeout`, `--ai`, `--ai-timeout`, `--ai-max-input-bytes`, `--ai-redact-secrets` | load with `posflag.ProviderWithFlag` |
| Rule option shorthands | `--max-lines`, `--skip-blank-lines`, `--skip-comments` | load with `posflag.ProviderWithFlag` into canonical `rules.tally.max-lines.*` keys |
| Transforming config flags | `--hide-source`, `--no-inline-directives`, `--acp-command` | apply through `lintOptions` after config decode; parse `--acp-command` with `mvdan.cc/sh/v3` or a shared shell helper if compatibility can be preserved |
| Appending rule selection flags | `--select`, `--ignore` | apply through `lintOptions` after config decode to preserve append semantics |
| Operational flags | `--config`, `--context`, `--target`, `--service`, `--exclude`, `--no-color`, `--fix`, `--fix-rule`, `--fix-unsafe` | keep outside koanf |

Special attention:

- `--config` is a tally config file path in standalone mode, but Docker has a global `--config` in plugin mode. Phase 1 should preserve
  `tally lint --config .tally.toml`; Phase 2 should rely on Docker's plugin helper to parse `docker --config ...` before `lint`, so
  `docker lint --config .tally.toml` remains a tally lint flag.
- `--context` is a tally build context flag in standalone mode, but Docker has a global `--context`. Phase 1 should preserve
  `tally lint --context DIR`; Phase 2 should rely on Docker's plugin helper to separate `docker --context prod lint ...` from
  `docker lint --context DIR ...`.
- `--show-source` defaults to true and must continue to support explicit false values if the current CLI supports them.
- `--hide-source` remains the clearer user-facing way to disable source snippets.
- string-slice env parsing must be tested for `TALLY_EXCLUDE`, `TALLY_FIX_RULE`, and any preserved slice env aliases. Do not assume Cobra's
  `StringSliceVar` exactly matches `urfave/cli/v3` env parsing.
- `--format` validation should move to Cobra `PreRunE` or the beginning of `RunE`.

Potential improvement: after Phase 1, unsupported `TALLY_*` values should be handled in exactly one place. Today, env-backed CLI flags and koanf
env loading overlap for some names. For example, `TALLY_RULES_SELECT` is a CLI compatibility alias for `--select`, not a config-shaped
`rules.*` value. Keeping CLI-only env aliases out of koanf avoids accidental schema-validation failures and makes precedence easier to reason
about.

### Viper Migration Assessment

Viper is worth considering because it is the companion package to Cobra and provides native helpers for common Cobra/pflag workflows:

- `BindPFlag` / `BindPFlags` for changed-flag precedence
- `BindEnv`, `AutomaticEnv`, `SetEnvPrefix`, and `SetEnvKeyReplacer` for env variables
- defaults, aliases, config file loading, and optional config watching in one API

The dependency footprint is not the main issue. The repository already receives `github.com/spf13/viper` indirectly today, and Viper itself uses
the same `github.com/go-viper/mapstructure/v2` decoder family that koanf v2 uses. A Viper migration would mostly be a behavior migration, not a
dependency-size decision.

Recommendation: do not migrate from koanf to Viper in the same phase as the Cobra migration. Phase 1 should be Cobra plus the existing koanf config
loader, with `posflag.ProviderWithFlag` added where direct config-shaped flags need Cobra/pflag integration. Revisit Viper only as a separate
config refactor after Cobra parity tests are stable.

Reasons to keep koanf in Phase 1:

- The current custom config logic is tally domain behavior, not accidental koanf boilerplate. Viper would not remove closest-config discovery,
  compatibility aliases, nested rule-table normalization, generated schema coercion/validation, per-rule option validation, unknown `TALLY_*`
  filtering, or LSP/editor override precedence.
- koanf's provider model matches the current explicit source ordering: defaults, filesystem config, env, editor overrides, and later CLI flags.
  `confmap.Provider` already supports the LSP override modes without involving CLI state.
- koanf preserves map key case through the raw config path. That matters because tally rule keys are externally visible and case-sensitive, for
  example BuildKit rule IDs like `StageNameCasing` and compatibility rule IDs like `DL4000` / `SC2086`.
- Viper reads config into an internal case-insensitive key map. That behavior is convenient for app settings, but it is risky for tally's rule
  namespace because it can collapse or rewrite case-sensitive rule IDs before the rule decoder sees them.
- Viper's `AutomaticEnv` and `Unmarshal` story has improved, but the struct-binding path is still experimental in v1.21. A stable migration would
  likely require explicit `BindEnv` mappings for every env key, which is not materially simpler than the current transform and validation layer.
- A one-phase Cobra plus Viper migration would move CLI parsing, env parsing, config decoding, LSP override precedence, and plugin preparation at
  the same time. Regressions would be harder to isolate.

Viper could still be useful later if the project wants a dedicated config redesign. A follow-up spike should prove these behaviors before changing
the main loader:

- case-preserving rule ID decoding for `buildkit/StageNameCasing`, `hadolint/DL4000`, `shellcheck/SC2086`, and tally rule option maps
- exact precedence parity for CLI flags, `TALLY_*` env vars, config files, and LSP overrides
- explicit handling for CLI-only env aliases that should not enter schema validation
- removal or retention plan for koanf use in rule option decoding helpers

### Phase 1 Acceptance Criteria

- `go test ./cmd/tally/cmd ./internal/config ./internal/integration/...` passes.
- `go run . lint --help`, `go run . lsp --help`, and `go run . version --help` remain sensible.
- Existing documented examples keep working:
  - `tally lint Dockerfile`
  - `tally lint --max-lines 100 Dockerfile`
  - `tally lint --config .tally.toml Dockerfile`
  - `tally lint --context . Dockerfile`
  - `tally version --json`
  - `tally lsp --stdio`
- Exit codes remain unchanged.
- Env behavior is covered by tests for:
  - `NO_COLOR`
  - `TALLY_FORMAT` and `TALLY_OUTPUT_FORMAT`
  - `TALLY_CONTEXT`
  - `TALLY_EXCLUDE`
  - `TALLY_RULES_SELECT`
  - `TALLY_RULES_IGNORE`
  - `TALLY_NO_INLINE_DIRECTIVES`
  - `TALLY_SLOW_CHECKS`
  - `TALLY_FIX`
  - `TALLY_FIX_RULE`
  - `TALLY_AI_*`
- The lint pipeline receives a framework-neutral options object or interface rather than a Cobra command where practical.

## Phase 2: Docker CLI Plugin

Phase 2 adds Docker plugin behavior on top of the Cobra CLI from Phase 1.

### Plugin Helper

Use Docker's Cobra plugin helper:

```go
plugin.Run(func(dockerCLI command.Cli) *cobra.Command {
    return newDockerLintPluginCommand(dockerCLI)
}, metadata.Metadata{
    SchemaVersion: "0.1.0",
    Vendor:        "Wharflab",
    Version:       version.Version(),
    ShortDescription: "Lint Dockerfiles and Containerfiles",
    URL:           "https://tally.wharflab.com/",
})
```

The helper should own:

- Docker global flag parsing
- Docker config and current-context resolution
- metadata command handling
- Docker-style help and completion behavior
- Docker CLI parent cancellation handling

Do not manually reproduce the plugin wrapper unless the helper becomes incompatible with tally's needs.

### Entrypoint Selection

The released artifact remains one compiled binary. `main()` should dispatch by executable basename:

- `tally` runs the standalone Cobra root
- `docker-lint` / `docker-lint.exe` runs `plugin.Run(...)` with a Cobra command named `lint`

This dispatch should trim the Windows `.exe` suffix. It should not rely on `DOCKER_CLI_PLUGIN_ORIGINAL_CLI_COMMAND`; that env var is useful for
Docker re-exec behavior after plugin invocation has already been selected, but the local binary name is the stable way to choose standalone vs
plugin mode.

### Plugin Command Shape

The plugin command should be the lint command:

```sh
docker lint Dockerfile
docker lint --format json Dockerfile
docker --context prod lint Dockerfile
docker lint --context . Dockerfile
```

The last two examples intentionally mean different things:

- `docker --context prod lint Dockerfile` selects Docker's current context before invoking the plugin.
- `docker lint --context . Dockerfile` passes tally's build context flag to the lint command.

The plugin command should not expose the standalone `lsp` or `version` subcommands under `docker lint`.

### Docker Context Use

The immediate MVP does not need the Docker daemon. However, the plugin should capture Docker CLI context metadata made available by
`command.Cli`:

- current Docker context name
- Docker config path
- Docker endpoint metadata if initialized without daemon contact

This data should be optional and read-only. It can be attached to future `BuildInvocation` metadata, reporter metadata, or debug output, but lint
rules should not depend on daemon access in the MVP.

### Metadata Command

`docker-lint docker-cli-plugin-metadata` is handled by Docker's helper and should return the upstream `metadata.Metadata` JSON.

Expected shape:

```json
{
  "SchemaVersion": "0.1.0",
  "Vendor": "Wharflab",
  "Version": "0.0.0",
  "ShortDescription": "Lint Dockerfiles and Containerfiles",
  "URL": "https://tally.wharflab.com/"
}
```

The metadata command must not require Docker daemon access and must not emit logs or banners around the JSON payload.

### Exit Codes

`docker lint` should return the same exit codes as `tally lint`:

| Exit code | Meaning |
|---|---|
| `0` | no violations, or below fail threshold |
| `1` | violations at or above fail level |
| `2` | config, CLI, parse, or unsupported invocation error |
| `3` | no Dockerfiles found |
| `4` | fatal Dockerfile syntax issue |

`docker-lint docker-cli-plugin-metadata` should return `0` unless JSON encoding or stdout writing fails.

---

## Packaging Plan

### Release Artifacts

Do not add a second compiled artifact for the MVP. Use one binary with two invocation names:

- `tally`
- `docker-lint`

The released archives can continue to include only `tally`. The implementation decision is:

- use a **symlink** on Unix-like systems when the source `tally` binary has a package-managed stable path
- use a **copy** on Windows
- use a **copy** for one-off manual installs from downloaded release assets unless the user intentionally wants to manage their own symlink

Why:

- Unix symlinks preserve one physical binary and automatically follow package upgrades.
- Homebrew's Docker plugin precedent is symlink-based.
- Windows symlink creation can require Developer Mode or elevated permissions, and symlink behavior is less reliable for ordinary CLI package
  installs.
- Copying on Windows matches Docker's own manual plugin guidance and this repo's release workflow practice for installing Buildx during Windows
  release tests.

Reasoning:

- avoids growing release archives
- avoids duplicate code signing/notarization work on macOS
- keeps NPM, PyPI, RubyGems, and WinGet packages focused on the standalone CLI
- gives Homebrew the exact integration users are asking for

Future option: add `docker-lint` to archives if enough non-Homebrew users want a no-symlink manual install.

### Homebrew Formula Extension

Current template: `packaging/homebrew/tally.rb.template`

Add plugin symlink installation:

```ruby
def install
  bin.install "tally"
  (lib/"docker/cli-plugins").install_symlink bin/"tally" => "docker-lint"
end
```

Add caveats matching the Buildx/Compose pattern:

```ruby
def caveats
  <<~EOS
    tally is also installed as a Docker CLI plugin. For Docker to find the plugin,
    add "cliPluginsExtraDirs" to ~/.docker/config.json:

      "cliPluginsExtraDirs": [
          "#{HOMEBREW_PREFIX}/lib/docker/cli-plugins"
      ]
  EOS
end
```

Notes:

- `#{HOMEBREW_PREFIX}` handles `/opt/homebrew`, `/usr/local`, and Linuxbrew prefixes.
- Do not edit the user's Docker config during `brew install`.
- Do not install into `~/.docker/cli-plugins` from the formula. Homebrew core formulae avoid writing into a user's home directory.
- The Docker CLI requires absolute paths for `cliPluginsExtraDirs`; the Homebrew prefix path satisfies this.

Update the formula test while touching this file. The current template calls `tally check`, but the command family in this repo is `tally lint`.
The plugin-specific test should not require a running Docker daemon or even the Docker CLI:

```ruby
metadata = shell_output("#{lib}/docker/cli-plugins/docker-lint docker-cli-plugin-metadata")
assert_match "\"SchemaVersion\":\"0.1.0\"", metadata
assert_match "\"Vendor\":\"Wharflab\"", metadata

output = shell_output("#{bin}/tally lint #{testpath}/Dockerfile --format json", 1)
assert_match "files_scanned", output
```

The expected exit status for the lint smoke test may be `1` if the fixture intentionally triggers rules. Use a fixture that is stable and assert
execution plus JSON shape rather than a clean lint result.

### Release Workflow Smoke Tests

Add plugin metadata smoke tests to `.github/workflows/release.yml` in the existing per-platform build job.

Linux and macOS:

```sh
PLUGIN_DIR="$(mktemp -d)"
ln -s "$(pwd)/${BINARY}" "${PLUGIN_DIR}/docker-lint"
"${PLUGIN_DIR}/docker-lint" docker-cli-plugin-metadata
```

Windows:

```powershell
$plugin = Join-Path $env:RUNNER_TEMP "docker-lint.exe"
Copy-Item $binary $plugin
& $plugin docker-cli-plugin-metadata
```

Validate:

- `SchemaVersion == "0.1.0"`
- `Vendor == "Wharflab"`
- `Version == expected release version`

Do not require `docker lint` to run in the release build smoke tests. The plugin contract can be validated by invoking the plugin executable
directly, which is deterministic across CI images.

### Other Package Managers

No MVP changes required for:

- NPM package can continue to expose `tally`
- PyPI package can continue to expose `tally`
- RubyGems package can continue to expose `tally`

Manual plugin installation should be documented for users of these packages:

```sh
mkdir -p ~/.docker/cli-plugins
ln -sf "$(command -v tally)" ~/.docker/cli-plugins/docker-lint
```

Windows manual install:

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.docker\cli-plugins"
Copy-Item (Get-Command tally.exe).Source "$env:USERPROFILE\.docker\cli-plugins\docker-lint.exe"
```

### WinGet Extension

Current files:

- `scripts/release/generate_winget_manifests.rb`
- `scripts/release/test_generate_winget_manifests.rb`

Current behavior:

- `InstallerType: portable`
- `Commands: ["tally"]`
- direct Windows `.exe` release assets are installed as the `tally` command

MVP recommendation:

1. Keep the package identifier `Wharflab.Tally`.
2. Keep `tally` as the primary WinGet command.
3. Do not add `docker-lint` to the WinGet portable `Commands` list; WinGet validation rejects multiple portable commands.
4. Do not try to make the WinGet manifest install directly into `%USERPROFILE%\.docker\cli-plugins`.
5. Do not edit Docker's `config.json` from WinGet.
6. Do not rely on Windows registry registration. Docker CLI plugin discovery does not consume registry entries.

This mirrors Docker's own WinGet precedent for Buildx and Compose: WinGet exposes a portable command, while Docker CLI plugin discovery is handled
by Docker plugin directories.

Documentation should offer the simple Windows path:

```powershell
winget install --id Wharflab.Tally -e
tally register-docker-plugin
docker lint --help
```

Users should rerun the registration after `winget upgrade Wharflab.Tally`.

---

## Documentation Plan

### New Documentation Page

Create:

- `_docs/integrations/docker-cli-plugin.mdx`

Add it to `_docs/docs.json` under the existing `Integrations` group:

```json
{
  "group": "Integrations",
  "pages": [
    "integrations/editorconfig",
    "integrations/docker-cli-plugin"
  ]
}
```

Recommended page title:

```mdx
---
title: Docker CLI plugin
description: Run tally as docker lint from the Docker CLI.
---
```

Recommended sections:

1. What It Does
   - `docker lint` is an alternate way to run `tally lint`.
   - It is a Docker CLI plugin, not a Docker Engine plugin.
2. Install With Homebrew
   - `brew install wharflab/tap/tally`
   - show the `cliPluginsExtraDirs` config snippet
   - verify with `docker lint --help`
3. Manual Install
   - macOS/Linux symlink into `~/.docker/cli-plugins/docker-lint`
   - Windows copy to `%USERPROFILE%\.docker\cli-plugins\docker-lint.exe`
4. Usage
   - Dockerfile, directory, Bake, Compose, JSON output, fix examples
5. Troubleshooting
   - `docker: 'lint' is not a docker command`
   - invalid plugin metadata
   - wrong architecture binary
   - missing execute bit on Unix
   - command name conflict with another `docker-lint`
6. Limitations
   - `docker lint` maps to `tally lint`
   - use `tally lsp --stdio` for editor/LSP workflows
   - use `tally version` for full standalone version details if `docker lint --version` is not enough

### Installation Page Cross-Link

Update `_docs/installation.mdx` Homebrew section:

- mention that Homebrew installs the `docker-lint` plugin symlink
- link to `/integrations/docker-cli-plugin`
- keep the primary install path focused on the standalone CLI

### Quickstart Cross-Link

Optionally update `_docs/quickstart.mdx` with one short example:

```sh
docker lint Dockerfile
```

Do not make Docker plugin usage the default quickstart path. The standalone `tally` CLI remains the lowest-friction cross-platform entrypoint.

---

## Test Plan

### Phase 1 Tests

Add or update command tests for the Cobra migration:

- command tree shape:
  - root has `lint`, `lsp`, and `version`
  - `lint` has all current flags
  - `lsp --stdio` keeps the same behavior
  - `version --json` keeps the same JSON behavior
- flag parsing and explicit-set behavior:
  - `--format`
  - `--show-source=false`
  - `--hide-source`
  - `--fix`
  - `--fix-rule`
  - `--target`
  - `--service`
- env behavior:
  - `NO_COLOR`
  - `TALLY_FORMAT`
  - `TALLY_OUTPUT_FORMAT`
  - `TALLY_CONTEXT`
  - `TALLY_EXCLUDE`
  - `TALLY_FIX`
  - `TALLY_FIX_RULE`
  - representative `TALLY_AI_*` env vars
- config precedence:
  - CLI flag beats env
  - env beats config file
  - config file beats defaults
  - CLI-only env aliases do not get loaded into koanf as invalid config

### Phase 2 Tests

Add focused tests for Docker plugin behavior:

- metadata command emits valid JSON
- metadata command includes version from `internal/version`
- Docker helper plugin command exposes lint behavior
- standalone root does not expose `docker-cli-plugin-metadata`
- executable-basename dispatch selects plugin mode for `docker-lint` and `docker-lint.exe`
- plugin command handles Docker-style argv with the command name present
- plugin command distinguishes Docker global flags from tally lint flags

Add integration coverage that builds the binary once and invokes it through a temporary plugin name.

Example flow:

1. build test binary as usual
2. create a temp directory
3. symlink binary to temp `docker-lint` on Linux/macOS; copy binary to temp `docker-lint.exe` on Windows
4. run `docker-lint docker-cli-plugin-metadata`
5. run `docker-lint lint --help`
6. run `docker-lint lint Dockerfile --format json`

These tests do not need a Docker daemon.

### Optional Docker CLI Discovery Smoke

Add a best-effort local script or documented manual smoke test:

```sh
tmp="$(mktemp -d)"
mkdir -p "$tmp/cli-plugins"
ln -s "$(pwd)/tally" "$tmp/cli-plugins/docker-lint"
DOCKER_CONFIG="$tmp" docker lint --help
```

This should not be required in CI unless the Docker CLI is guaranteed to be available and stable. The direct plugin contract tests provide most of
the safety at lower cost.

### Homebrew Formula Tests

The generated formula artifact should be checked in PR release dry runs as it is today. The formula test should include:

- standalone `tally version`
- standalone `tally lint` smoke
- direct plugin metadata smoke through `#{lib}/docker/cli-plugins/docker-lint`

Do not test `docker lint` inside the Homebrew formula test. Homebrew test environments should not depend on Docker being installed or configured.

---

## Implementation Phases

### Phase 1: Migrate Standalone CLI To Cobra

Files:

- `main.go`
- `cmd/tally/cmd/root.go`
- `cmd/tally/cmd/lint.go`
- `cmd/tally/cmd/lsp.go`
- `cmd/tally/cmd/version.go`
- optional `cmd/tally/cmd/options.go`
- command tests in `cmd/tally/cmd`

Tasks:

- replace `urfave/cli/v3` command construction with Cobra
- preserve the standalone command tree and help output
- introduce a framework-neutral `lintOptions` struct or interface
- replace `cli.Exit("", code)` with an equivalent typed error / status-code path
- preserve `cmd.IsSet` semantics through `posflag.ProviderWithFlag` for config-shaped flags and pointer-valued options or pflag `Changed` for
  operational flags
- keep koanf in Phase 1; do not combine the CLI framework migration with a Viper config migration
- move env-backed CLI-only aliases out of framework declarations and into explicit option loading
- keep koanf as the owner of config-shaped `TALLY_*` env vars
- replace or validate the current `--acp-command` splitter against `mvdan.cc/sh/v3` so quoting behavior is not reimplemented by hand during the
  framework migration
- add tests covering flag/env/config precedence

Acceptance criteria:

- `go test ./cmd/tally/cmd` passes
- `go run . lint --help` still shows the standalone CLI
- representative integration snapshots remain unchanged unless the help text intentionally changes
- documented standalone examples keep working
- no Docker plugin behavior exists yet

### Phase 2: Add Docker CLI Plugin Support

Files:

- `cmd/tally/cmd/docker_plugin.go`
- `cmd/tally/cmd/docker_plugin_test.go`
- `.github/workflows/release.yml`
- `packaging/homebrew/tally.rb.template`
- `scripts/release/generate_winget_manifests.rb`
- `scripts/release/test_generate_winget_manifests.rb`
- `_docs/integrations/docker-cli-plugin.mdx`
- `_docs/docs.json`
- `_docs/installation.mdx`
- optional `_docs/quickstart.mdx`

Tasks:

- add executable-basename dispatch for `tally` vs `docker-lint`
- add Docker plugin entrypoint using `github.com/docker/cli/cli-plugins/plugin`
- build plugin command from the Cobra lint command and shared `lintOptions`
- provide `metadata.Metadata` with version, vendor, description, and URL
- capture Docker current context/config metadata as optional invocation metadata
- add direct executable plugin tests
- add per-platform release smoke for metadata
- add Homebrew `docker-lint` symlink under `lib/docker/cli-plugins`
- add Homebrew caveats for `cliPluginsExtraDirs`
- fix formula test to use `tally lint`
- keep generated WinGet `Commands` limited to `tally`
- update docs with Homebrew, WinGet, and manual install paths

Acceptance criteria:

- `tally lint Dockerfile` still works unchanged
- a Unix symlink named `docker-lint` can run `docker-cli-plugin-metadata`
- a Windows copy named `docker-lint.exe` can run `docker-cli-plugin-metadata`
- `docker lint Dockerfile` works after Docker discovers the plugin
- `docker --context NAME lint Dockerfile` does not collide with tally's `docker lint --context DIR Dockerfile`
- CI validates plugin metadata on Linux, macOS, and Windows release builds
- generated Homebrew formula follows the Buildx/Compose plugin pattern
- generated WinGet manifests validate
- docs do not imply that `winget install Wharflab.Tally` alone makes `docker lint` work unless that exact path has been verified

### Phase 2 Subtasks

These are tracked under Phase 2, not separate phases.

#### Integration And Release Smoke

Files:

- `internal/integration/...` if an existing CLI harness can cover this cleanly
- `.github/workflows/release.yml`

Tasks:

- add direct executable plugin tests
- add per-platform release smoke for metadata
- ensure version injection appears in plugin metadata

Acceptance criteria:

- CI validates plugin metadata on Linux, macOS, and Windows release builds
- no release archive format change is required

#### Homebrew Formula

Files:

- `packaging/homebrew/tally.rb.template`
- possibly `scripts/release/test_*.rb` or a new formula-generation test if the repo wants coverage

Tasks:

- add `docker-lint` symlink under `lib/docker/cli-plugins`
- add caveats for `cliPluginsExtraDirs`
- fix formula test to use `tally lint`
- add plugin metadata assertion

Acceptance criteria:

- generated formula follows the Buildx/Compose plugin pattern
- no formula code writes into user home directories
- formula test does not require Docker

#### WinGet Manifest

Files:

- `scripts/release/generate_winget_manifests.rb`
- `scripts/release/test_generate_winget_manifests.rb`

Tasks:

- keep generated WinGet `Commands` limited to `tally`
- update manifest tests to reject the invalid multi-command portable installer shape
- document `tally register-docker-plugin` as the WinGet plugin setup path

Acceptance criteria:

- `winget validate --manifest` accepts the generated manifests
- `winget install --manifest` exposes `tally`
- `winget install --manifest` does not attempt to expose `docker-lint` as a second portable command
- docs do not imply that `winget install Wharflab.Tally` alone makes `docker lint` work unless that exact path has been verified

#### Documentation

Files:

- `_docs/integrations/docker-cli-plugin.mdx`
- `_docs/docs.json`
- `_docs/installation.mdx`
- optional `_docs/quickstart.mdx`

Tasks:

- add Docker CLI plugin integration page
- add docs navigation entry
- cross-link from installation
- include Homebrew and manual install paths
- include troubleshooting

Acceptance criteria:

- docs make it clear this is a CLI plugin, not an Engine plugin
- Homebrew instructions use `cliPluginsExtraDirs`
- examples show `docker lint` for Dockerfile, Bake, and Compose inputs

---

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Cobra migration changes env/config precedence | add explicit flag/env/config precedence tests before plugin work |
| `cmd.IsSet` behavior changes because pflag only tracks command-line input | use koanf `posflag` for config-shaped flags; represent explicit state in `lintOptions` and load CLI-only env aliases deliberately |
| Viper migration hides CLI migration regressions | keep Viper out of Phase 1; evaluate it only in a dedicated config spike |
| Viper lowercases case-sensitive rule IDs | keep koanf unless a prototype proves exact case-preserving decoding for BuildKit, Hadolint, ShellCheck, and tally rule keys |
| koanf-loaded CLI flags replace existing append/transform semantics | keep `--select`, `--ignore`, `--hide-source`, `--no-inline-directives`, and `--acp-command` outside the direct `posflag` path |
| CLI-only env vars accidentally enter koanf and fail schema validation | keep CLI-only env handling outside koanf or add explicit config mappings before enabling them |
| `tally lint --context` collides conceptually with Docker global `--context` | use Docker's plugin helper so `docker --context X lint ...` is parsed separately from `docker lint --context DIR ...` |
| `docker lint` conflicts with another plugin or future Docker command | document conflict troubleshooting; keep `tally lint` as the stable primary CLI |
| Docker cannot find Homebrew-installed plugin | use the same `cliPluginsExtraDirs` caveat pattern as Buildx and Compose |
| Plugin metadata command accidentally runs lint on a file named `docker-cli-plugin-metadata` | use Docker's plugin helper and test metadata probing directly |
| Formula test starts depending on Docker | test the plugin executable directly, not `docker lint` |
| Release metadata has wrong version | add release smoke asserting metadata `Version` against `RELEASE_VERSION` |
| Plugin root drifts from `tally lint` flags | build plugin root from shared Cobra lint command/options and add representative flag tests |
| Symlink invocation is not detected on Windows | trim `.exe` in detection and copy binary to `docker-lint.exe` in Windows tests |
| WinGet cannot expose both `tally` and `docker-lint` as portable commands | keep WinGet focused on `tally` and document `tally register-docker-plugin` |

---

## Open Questions

1. Should the project also support `docker tally` later?

   Recommendation: not in the MVP. It is safer from a naming perspective, but it duplicates surface area and makes docs less focused.

2. Should release archives include both `tally` and `docker-lint`?

   Recommendation: not in the MVP. Homebrew can provide the symlink, and manual install docs are enough for other package managers.

3. Should WinGet expose `docker-lint` in addition to `tally`?

   Recommendation: no. Current WinGet validation rejects multiple `Commands` values for portable installers, so WinGet should expose `tally` and
   users should run `tally register-docker-plugin` for Docker CLI registration.

4. Should `docker lint --version` be guaranteed?

   Recommendation: support it if Cobra can do so without adding a positional `version` command under `docker lint`. Otherwise rely on metadata and
   `tally version` rather than adding a positional `version` command that conflicts with lint target paths.

5. Should the Cobra migration also move config from koanf to Viper?

   Recommendation: no. Cobra has enough value for Docker integration on its own, while Viper would be a separate config behavior migration. Keep
   Phase 1 scoped to Cobra plus koanf and revisit Viper only after a prototype proves case-preserving rule decoding and exact env/config/LSP
   precedence parity.

6. Should `docker lint` auto-discover Docker context from the Docker CLI?

   Recommendation: no. `docker lint` should remain a local static analysis command. Build invocation semantics should continue through the
   planned Bake/Compose support from design doc 38.

---

## Done Definition

The feature is complete when:

- standalone tally commands run on Cobra
- `tally lint Dockerfile` still works unchanged
- standalone flag, env, and config precedence is covered by tests
- a Unix symlink named `docker-lint` supports Docker metadata probing
- a Windows copy named `docker-lint.exe` supports Docker metadata probing
- `docker lint Dockerfile` works after Docker discovers the plugin
- Docker current context/config metadata is available to the plugin path without requiring daemon access
- Homebrew installs a `docker-lint` symlink under `#{HOMEBREW_PREFIX}/lib/docker/cli-plugins`
- Homebrew caveats explain `cliPluginsExtraDirs`
- WinGet keeps `tally` as the primary command and plugin registration is handled by `tally register-docker-plugin`
- docs include a dedicated Docker CLI plugin integration page
- tests cover metadata, invocation detection, plugin root behavior, and release version injection
