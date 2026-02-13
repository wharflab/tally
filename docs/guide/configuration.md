# Configuration

tally supports configuration via TOML config files, environment variables, and CLI flags. Configuration sources cascade in a predictable order, making
it easy to set project defaults while allowing per-run overrides.

## Quick Start

Create a `.tally.toml` in your project root:

```toml
[output]
format = "text"
show-source = true

[rules]
include = ["buildkit/*", "tally/*", "hadolint/*"]
exclude = ["buildkit/MaintainerDeprecated"]

[rules.tally.max-lines]
max = 100
```

Then run:

```bash
tally lint .
```

## Config File

### File Names

tally looks for these config file names (in order):

1. `.tally.toml` (hidden file, recommended)
2. `tally.toml`

### Discovery

tally uses cascading config discovery similar to [Ruff](https://docs.astral.sh/ruff/configuration/):

1. Starting from the Dockerfile's directory, walks up the filesystem
2. Stops at the first `.tally.toml` or `tally.toml` found
3. Uses that config (no merging with parent configs)

This allows monorepo setups with per-directory configurations:

```text
monorepo/
├── .tally.toml              # Default config for most services
├── services/
│   ├── api/
│   │   └── Dockerfile       # Uses monorepo/.tally.toml
│   └── legacy/
│       ├── .tally.toml      # Override for legacy service
│       └── Dockerfile       # Uses services/legacy/.tally.toml
```

### Explicit Config Path

Override discovery with `--config`:

```bash
tally lint --config /path/to/.tally.toml Dockerfile
```

## Priority Order

Configuration sources are applied in this order (highest priority first):

1. **CLI flags** (`--max-lines 100`)
2. **Environment variables** (`TALLY_RULES_MAX_LINES_MAX=100`)
3. **Config file** (`.tally.toml` or `tally.toml`)
4. **Built-in defaults**

## Config File Reference

### Output Section

Controls how tally reports violations.

```toml
[output]
format = "text"           # text, json, sarif, github-actions, markdown
path = "stdout"           # stdout, stderr, or file path
show-source = true        # Show source code snippets
fail-level = "style"      # Minimum severity for exit code 1
```

| Option | Default | Description |
|--------|---------|-------------|
| `format` | `"text"` | Output format: `text`, `json`, `sarif`, `github-actions`, `markdown` |
| `path` | `"stdout"` | Output destination: `stdout`, `stderr`, or a file path |
| `show-source` | `true` | Show source code snippets with violations |
| `fail-level` | `"style"` | Minimum severity for non-zero exit: `error`, `warning`, `info`, `style`, `none` |

### Rules Section

Controls which rules are enabled and their configuration.

```toml
[rules]
include = ["buildkit/*", "tally/*"]           # Enable rules by namespace or specific rule
exclude = ["buildkit/MaintainerDeprecated"]   # Disable specific rules
```

#### Rule Selection

Use glob patterns to include/exclude rules:

```toml
[rules]
# Enable entire namespaces
include = ["buildkit/*", "tally/*", "hadolint/*"]

# Disable specific rules
exclude = [
  "buildkit/MaintainerDeprecated",
  "hadolint/DL3008",
]
```

#### Per-Rule Configuration

Configure individual rules with severity and options:

```toml
# Syntax: [rules.<namespace>.<rule-name>]

[rules.tally.max-lines]
severity = "error"           # Override severity
max = 500                    # Rule-specific option
skip-blank-lines = true
skip-comments = true

[rules.buildkit.StageNameCasing]
severity = "info"            # Downgrade from warning to info

[rules.hadolint.DL3026]
severity = "warning"
trusted-registries = ["docker.io", "gcr.io", "ghcr.io"]
```

#### Severity Levels

| Severity | Description |
|----------|-------------|
| `"off"` | Disable the rule |
| `"error"` | Critical issues that should block CI |
| `"warning"` | Important issues that should be addressed |
| `"info"` | Informational suggestions |
| `"style"` | Style preferences |

#### Enabling "Off by Default" Rules

Some rules are disabled by default (e.g., experimental rules). Enable them by providing configuration:

```toml
# DL3026 is off by default, providing config auto-enables it
[rules.hadolint.DL3026]
trusted-registries = ["docker.io", "ghcr.io"]
# Auto-enabled with severity="warning"

# Or explicitly set severity
[rules.tally.prefer-copy-heredoc]
severity = "style"           # Enables the experimental rule
```

### Inline Directives Section

Controls how inline ignore comments are processed.

```toml
[inline-directives]
enabled = true              # Process inline directives (default: true)
warn-unused = false         # Warn about unused directives (default: false)
validate-rules = false      # Warn about unknown rule codes (default: false)
require-reason = false      # Require reason= on all ignore directives (default: false)
```

## Environment Variables

All configuration can be set via environment variables:

### Output Variables

| Variable | Description |
|----------|-------------|
| `TALLY_OUTPUT_FORMAT` | Output format (`text`, `json`, `sarif`, `github-actions`, `markdown`) |
| `TALLY_OUTPUT_PATH` | Output destination (`stdout`, `stderr`, or file path) |
| `TALLY_OUTPUT_SHOW_SOURCE` | Show source snippets (`true`/`false`) |
| `TALLY_OUTPUT_FAIL_LEVEL` | Minimum severity for non-zero exit |
| `NO_COLOR` | Disable colored output (standard env var) |

### Rule Variables

| Variable | Description |
|----------|-------------|
| `TALLY_RULES_MAX_LINES_MAX` | Maximum lines allowed |
| `TALLY_RULES_MAX_LINES_SKIP_BLANK_LINES` | Exclude blank lines (`true`/`false`) |
| `TALLY_RULES_MAX_LINES_SKIP_COMMENTS` | Exclude comments (`true`/`false`) |

### File Discovery Variables

| Variable | Description |
|----------|-------------|
| `TALLY_EXCLUDE` | Glob pattern(s) to exclude files (comma-separated) |
| `TALLY_CONTEXT` | Build context directory for context-aware rules |

### Directive Variables

| Variable | Description |
|----------|-------------|
| `TALLY_NO_INLINE_DIRECTIVES` | Disable inline directive processing (`true`/`false`) |
| `TALLY_INLINE_DIRECTIVES_WARN_UNUSED` | Warn about unused directives (`true`/`false`) |
| `TALLY_INLINE_DIRECTIVES_REQUIRE_REASON` | Require reason= on ignore directives (`true`/`false`) |

## CLI Flags

### Core Flags

| Flag | Description |
|------|-------------|
| `--config, -c` | Path to config file (overrides discovery) |
| `--exclude` | Glob pattern(s) to exclude files |
| `--context` | Build context directory for context-aware rules |

### Output Flags

| Flag | Description |
|------|-------------|
| `--format, -f` | Output format: `text`, `json`, `sarif`, `github-actions`, `markdown` |
| `--output, -o` | Output destination: `stdout`, `stderr`, or file path |
| `--no-color` | Disable colored output |
| `--show-source` | Show source code snippets (default: true) |
| `--hide-source` | Hide source code snippets |
| `--fail-level` | Minimum severity for non-zero exit |

### Rule Flags

| Flag | Description |
|------|-------------|
| `--max-lines, -l` | Maximum number of lines allowed (0 = unlimited) |
| `--skip-blank-lines` | Exclude blank lines from line count |
| `--skip-comments` | Exclude comment lines from line count |

### Directive Flags

| Flag | Description |
|------|-------------|
| `--no-inline-directives` | Disable processing of inline ignore directives |
| `--warn-unused-directives` | Warn about directives that don't suppress any violations |
| `--require-reason` | Warn about ignore directives without `reason=` explanation |

### Fix Flags

| Flag | Description |
|------|-------------|
| `--fix` | Apply safe auto-fixes |
| `--fix-unsafe` | Apply all auto-fixes including unsafe ones |
| `--diff` | Show diff of proposed fixes without applying |

## Inline Directives

Suppress specific violations using inline comment directives directly in your Dockerfile.

### Next-Line Directive

Suppress violations on the next line:

```dockerfile
# tally ignore=StageNameCasing
FROM alpine AS Build
```

### Global Directive

Suppress violations throughout the entire file:

```dockerfile
# tally global ignore=max-lines
FROM alpine
# ... rest of file is not checked for max-lines
```

### Multiple Rules

Suppress multiple rules with comma-separated values:

```dockerfile
# tally ignore=StageNameCasing,DL3006
FROM Ubuntu AS Build
```

### Adding Reasons

Document why a rule is being ignored using `;reason=`:

```dockerfile
# tally ignore=DL3006;reason=Using older base image for compatibility
FROM ubuntu:16.04

# tally global ignore=max-lines;reason=Generated file, size is expected
```

Use `--require-reason` to enforce that all ignore directives include an explanation.

### Suppressing All Rules

Use `all` to suppress all rules on a line:

```dockerfile
# tally ignore=all
FROM Ubuntu AS Build
```

### Migration Compatibility

tally supports directive formats from other linters:

```dockerfile
# hadolint ignore=DL3006
FROM ubuntu

# hadolint global ignore=DL3008
FROM alpine

# check=skip=StageNameCasing
FROM alpine AS Build
```

## Example Configurations

### Strict CI Configuration

```toml
# .tally.toml - Strict settings for CI
[output]
format = "sarif"
path = "tally-results.sarif"
fail-level = "warning"

[rules]
include = ["buildkit/*", "tally/*", "hadolint/*"]

[rules.tally.max-lines]
max = 50
skip-blank-lines = true
skip-comments = true

[inline-directives]
require-reason = true
warn-unused = true
```

### Relaxed Development Configuration

```toml
# .tally.toml - Relaxed settings for development
[output]
format = "text"
show-source = true
fail-level = "error"

[rules]
include = ["buildkit/*", "tally/*"]
exclude = ["buildkit/MaintainerDeprecated"]

[rules.tally.max-lines]
severity = "warning"
max = 200
```

### Legacy Project Migration

```toml
# .tally.toml - Gradual migration from hadolint
[output]
format = "text"
fail-level = "error"

[rules]
# Start with just BuildKit rules
include = ["buildkit/*"]
# Add hadolint rules gradually
# include = ["buildkit/*", "hadolint/DL3006", "hadolint/DL3007"]

[rules.buildkit.StageNameCasing]
severity = "info"  # Downgrade during migration
```
