# tally

[![codecov](https://codecov.io/gh/tinovyatkin/tally/graph/badge.svg?token=J3vK0hyLkf)](https://codecov.io/gh/tinovyatkin/tally)

tally keeps Dockerfiles and Containerfiles clean, modern, and consistent — using BuildKit's own parser and checks (the same foundation behind
`docker buildx`) plus safe auto-fixes. It runs fast, doesn't require Docker Desktop or a daemon, and fits neatly into CI. If that sounds like your
workflow, try `tally check .`.

```bash
# Lint everything in the repo (recursive)
tally check .

# Apply all safe fixes automatically
tally check --fix Dockerfile
```

## Why tally?

Dockerfile linting usually means picking a compromise:

- **Hadolint** is popular and battle-tested, but it uses its own Dockerfile parser, so support for newer BuildKit features can lag behind. It also
  is commonly consumed as a prebuilt binary, and it focuses on reporting — not fixing.
- **`docker buildx --check`** runs Docker's official BuildKit checks, but it requires the Docker/buildx toolchain and can be heavier than a pure
  static linter (and not always available if you're using Podman/Finch/other runtimes).

tally exists to bring modern linter ergonomics to container builds:

- **BuildKit-native parsing**: understands modern syntax like heredocs, `RUN --mount=...`, and `ADD --checksum=...`.
- **Fixes, not just findings**: applies safe, mechanical fixes automatically (`--fix`), with per-rule control when you need it.
- **Easy to install anywhere**: available via Homebrew, Go, npm, pip, and RubyGems — so it can flow through your existing artifact mirrors.
- **Container ecosystem friendly**: supports Dockerfile/Containerfile conventions and `.dockerignore`/`.containerignore`.
- **A growing ruleset**: combines official BuildKit checks, Hadolint-compatible rules, and tally-specific rules.

Roadmap: editor integrations (VS Code, Zed), more auto-fixes, and higher-level rules (cache & tmpfs mount recommendations, tooling-aware checks for
uv/bun, line-length and layer optimizations).

## Supported Rules

tally integrates rules from multiple sources:

<!-- BEGIN RULES_TABLE -->
| Source | Rules | Description |
|--------|-------|-------------|
| **[BuildKit](https://docs.docker.com/reference/build-checks/)** | 22 rules | Docker's official Dockerfile checks (automatically captured) |
| **tally** | 3 rules | Custom rules including secret detection with [gitleaks](https://github.com/gitleaks/gitleaks) |
| **[Hadolint](https://github.com/hadolint/hadolint)** | 23 rules | Hadolint-compatible Dockerfile rules (expanding) |
<!-- END RULES_TABLE -->

**See [RULES.md](RULES.md) for the complete rules reference.**

## Installation

### Homebrew (macOS/Linux)

```bash
brew install tinovyatkin/tap/tally
```

### NPM

```bash
npm install -g tally-cli
```

### PyPI

```bash
pip install tally-cli
```

### RubyGems

```bash
gem install tally-cli
```

### Go

```bash
go install github.com/tinovyatkin/tally@latest
```

### From Source

```bash
git clone https://github.com/tinovyatkin/tally.git
cd tally
go build .
```

## Usage

```bash
# Check a Dockerfile
tally check Dockerfile

# Check all Dockerfiles in current directory (recursive)
tally check .

# Check with glob patterns
tally check "**/*.Dockerfile"

# Exclude patterns
tally check --exclude "vendor/*" --exclude "test/*" .

# Check with max lines limit
tally check --max-lines 100 Dockerfile

# Output as JSON
tally check --format json Dockerfile

# Check multiple files
tally check Dockerfile.dev Dockerfile.prod

# Enable context-aware rules (e.g., copy-ignored-file)
tally check --context . Dockerfile
```

### File Discovery

When given a directory, tally recursively searches for Dockerfiles using these default patterns:

- `Dockerfile`
- `Dockerfile.*` (e.g., `Dockerfile.dev`, `Dockerfile.prod`)
- `*.Dockerfile` (e.g., `api.Dockerfile`, `frontend.Dockerfile`)
- `Containerfile` (Podman convention)
- `Containerfile.*`
- `*.Containerfile`

Use `--exclude` to filter out unwanted files:

```bash
# Exclude vendor and test directories
tally check --exclude "vendor/*" --exclude "test/*" .

# Exclude all .bak files
tally check --exclude "*.bak" .
```

## Rules Overview

For the complete list of all supported rules, see **[RULES.md](RULES.md)**.

### Context-Aware Rules

Some rules require build context awareness. Enable them with the `--context` flag:

```bash
# Enable context-aware rules
tally check --context . Dockerfile
```

**copy-ignored-file**: Detects when `COPY` or `ADD` commands reference files that would be excluded by `.dockerignore`. This helps catch mistakes
where files are copied but won't actually be included in the build.

```dockerfile
# .dockerignore contains: *.log

# This will trigger a warning:
COPY app.log /app/  # File matches .dockerignore pattern

# Heredoc sources are exempt (they're inline, not from context):
COPY <<EOF /app/config.txt
inline content
EOF
```

## Ignoring Violations

You can suppress specific violations using inline comment directives.

### Next-Line Directives

Suppress violations on the next line:

```dockerfile
# tally ignore=StageNameCasing
FROM alpine AS Build
```

### Global Directives

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

Document why a rule is being ignored using `;reason=` (BuildKit-style separator):

```dockerfile
# tally ignore=DL3006;reason=Using older base image for compatibility
FROM ubuntu:16.04

# tally global ignore=max-lines;reason=Generated file, size is expected
FROM alpine

# check=skip=StageNameCasing;reason=Legacy naming convention
FROM alpine AS Build
```

Use `--require-reason` to enforce that all ignore directives include an explanation:

```bash
tally check --require-reason Dockerfile
```

Note: The `;reason=` syntax is a tally extension that works with all directive formats. BuildKit silently ignores the `reason` option.

### Migration Compatibility

tally supports directive formats from other linters for easy migration:

```dockerfile
# hadolint ignore=DL3006
FROM ubuntu

# hadolint global ignore=DL3008
FROM alpine

# check=skip=StageNameCasing
FROM alpine AS Build
```

### Suppressing All Rules

Use `all` to suppress all rules on a line:

```dockerfile
# tally ignore=all
FROM Ubuntu AS Build
```

### CLI Options

| Flag                       | Description                                                |
| -------------------------- | ---------------------------------------------------------- |
| `--no-inline-directives`   | Disable processing of inline ignore directives             |
| `--warn-unused-directives` | Warn about directives that don't suppress any violations   |
| `--require-reason`         | Warn about ignore directives without `reason=` explanation |

### Configuration

Inline directive behavior can be configured in `.tally.toml`:

```toml
[inline-directives]
enabled = true        # Process inline directives (default: true)
warn-unused = false   # Warn about unused directives (default: false)
validate-rules = false # Warn about unknown rule codes (default: false)
require-reason = false # Require reason= on all ignore directives (default: false)
```

## Configuration

tally supports configuration via TOML config files, environment variables, and CLI flags.

### Config File

Create a `.tally.toml` or `tally.toml` file in your project:

```toml
[output]
format = "text"          # text, json, sarif, github-actions, markdown
path = "stdout"          # stdout, stderr, or file path
show-source = true       # Show source code snippets
fail-level = "style"     # Minimum severity for exit code 1

# Rule selection (Ruff-style)
[rules]
include = ["buildkit/*", "tally/*"]           # Enable rules by namespace or specific rule
exclude = ["buildkit/MaintainerDeprecated"]   # Disable specific rules

# Per-rule configuration (severity, options)
[rules.tally.max-lines]
severity = "error"
max = 500
skip-blank-lines = true
skip-comments = true

[rules.buildkit.StageNameCasing]
severity = "info"         # Downgrade severity

[rules.hadolint.DL3026]
severity = "warning"
trusted-registries = ["docker.io", "gcr.io"]
```

### Config File Discovery

tally uses cascading config discovery similar to [Ruff](https://docs.astral.sh/ruff/configuration/):

1. Starting from the Dockerfile's directory, walks up the filesystem
2. Stops at the first `.tally.toml` or `tally.toml` found
3. Uses that config (no merging with parent configs)

This allows monorepo setups with per-directory configurations.

### Priority Order

Configuration sources are applied in this order (highest priority first):

1. **CLI flags** (`--max-lines 100`)
2. **Environment variables** (`TALLY_RULES_MAX_LINES_MAX=100`)
3. **Config file** (`.tally.toml` or `tally.toml`)
4. **Built-in defaults**

### Environment Variables

| Variable                                 | Description                                                           |
| ---------------------------------------- | --------------------------------------------------------------------- |
| `TALLY_OUTPUT_FORMAT`                    | Output format (`text`, `json`, `sarif`, `github-actions`, `markdown`) |
| `TALLY_OUTPUT_PATH`                      | Output destination (`stdout`, `stderr`, or file path)                 |
| `TALLY_OUTPUT_SHOW_SOURCE`               | Show source snippets (`true`/`false`)                                 |
| `TALLY_OUTPUT_FAIL_LEVEL`                | Minimum severity for non-zero exit                                    |
| `NO_COLOR`                               | Disable colored output (standard env var)                             |
| `TALLY_EXCLUDE`                          | Glob pattern(s) to exclude files (comma-separated)                    |
| `TALLY_CONTEXT`                          | Build context directory for context-aware rules                       |
| `TALLY_RULES_MAX_LINES_MAX`              | Maximum lines allowed                                                 |
| `TALLY_RULES_MAX_LINES_SKIP_BLANK_LINES` | Exclude blank lines (`true`/`false`)                                  |
| `TALLY_RULES_MAX_LINES_SKIP_COMMENTS`    | Exclude comments (`true`/`false`)                                     |
| `TALLY_NO_INLINE_DIRECTIVES`             | Disable inline directive processing (`true`/`false`)                  |
| `TALLY_INLINE_DIRECTIVES_WARN_UNUSED`    | Warn about unused directives (`true`/`false`)                         |
| `TALLY_INLINE_DIRECTIVES_REQUIRE_REASON` | Require reason= on ignore directives (`true`/`false`)                 |

### CLI Flags

```bash
# Specify config file explicitly
tally check --config /path/to/.tally.toml Dockerfile

# Override max-lines from config
tally check --max-lines 200 Dockerfile

# Exclude blank lines and comments from count
tally check --max-lines 100 --skip-blank-lines --skip-comments Dockerfile
```

## Output Formats

tally supports multiple output formats for different use cases.

### Text (default)

Human-readable output with colors and source code snippets:

```bash
tally check Dockerfile
```

```text
WARNING: StageNameCasing - https://docs.docker.com/go/dockerfile/rule/stage-name-casing/
Stage name 'Builder' should be lowercase

Dockerfile:2
────────────────────
   1 │ FROM alpine
>>>2 │ FROM ubuntu AS Builder
   3 │ RUN echo "hello"
────────────────────
```

### JSON

Machine-readable format with summary statistics and scan metadata:

```bash
tally check --format json Dockerfile
```

The JSON output includes:

- `files`: Array of files with their violations
- `summary`: Aggregate statistics (total, errors, warnings, etc.)
- `files_scanned`: Total number of files scanned
- `rules_enabled`: Number of active rules (with `DefaultSeverity != "off"`)

```json
{
  "files": [
    {
      "file": "Dockerfile",
      "violations": [
        {
          "location": {
            "file": "Dockerfile",
            "start": { "line": 2, "column": 0 }
          },
          "rule": "buildkit/StageNameCasing",
          "message": "Stage name 'Builder' should be lowercase",
          "severity": "warning",
          "docUrl": "https://docs.docker.com/go/dockerfile/rule/stage-name-casing/"
        }
      ]
    }
  ],
  "summary": {
    "total": 1,
    "errors": 0,
    "warnings": 1,
    "info": 0,
    "style": 0,
    "files": 1
  },
  "files_scanned": 1,
  "rules_enabled": 35
}
```

### SARIF

[Static Analysis Results Interchange Format](https://docs.oasis-open.org/sarif/sarif/v2.1.0/) for CI/CD integration with GitHub Code Scanning, Azure DevOps, and other tools:

```bash
tally check --format sarif Dockerfile > results.sarif
```

### GitHub Actions

Native GitHub Actions workflow command format for inline annotations:

```bash
tally check --format github-actions Dockerfile
```

```text
::warning file=Dockerfile,line=2,title=StageNameCasing::Stage name 'Builder' should be lowercase
```

### Markdown

Concise Markdown tables optimized for AI agents and token efficiency:

```bash
tally check --format markdown Dockerfile
```

```markdown
**2 issues** in `Dockerfile`

| Line | Issue                                       |
| ---- | ------------------------------------------- |
| 10   | ❌ Use absolute WORKDIR                     |
| 2    | ⚠️ Stage name 'Builder' should be lowercase |
```

Features:

- Summary upfront with issue counts
- Sorted by severity (errors first)
- Emoji indicators: ❌ error, ⚠️ warning, ℹ️ info, 💅 style
- No rule codes or doc URLs (token-efficient)
- Multi-file support with File column when needed

### Output Options

| Flag            | Description                                                          |
| --------------- | -------------------------------------------------------------------- |
| `--format, -f`  | Output format: `text`, `json`, `sarif`, `github-actions`, `markdown` |
| `--output, -o`  | Output destination: `stdout`, `stderr`, or file path                 |
| `--no-color`    | Disable colored output (also respects `NO_COLOR` env var)            |
| `--show-source` | Show source code snippets (default: true)                            |
| `--hide-source` | Hide source code snippets                                            |

### Exit Codes

| Code | Meaning                                           |
| ---- | ------------------------------------------------- |
| `0`  | No violations (or below `--fail-level` threshold) |
| `1`  | Violations found at or above `--fail-level`       |
| `2`  | Parse or configuration error                      |

### Fail Level

Control which severity levels cause a non-zero exit code:

```bash
# Fail only on errors (ignore warnings)
tally check --fail-level error Dockerfile

# Never fail (useful for CI reporting without blocking)
tally check --fail-level none --format sarif Dockerfile > results.sarif

# Fail on any violation including style issues (default behavior)
tally check --fail-level style Dockerfile
```

Available levels (from most to least severe): `error`, `warning`, `info`, `style` (default), `none`

## Development

### Running Tests

```bash
# Run all tests
make test

# Run linting
make lint

# Run copy/paste detection (CPD)
make cpd
```

### Code Quality

This project uses:

- **golangci-lint** for Go linting
- **PMD CPD** for copy/paste detection (minimum 100 tokens)

Copy/paste detection runs automatically in CI and helps identify duplicate code patterns.

## Contributing

See [CLAUDE.md](CLAUDE.md) for development guidelines.

## License

Apache-2.0
