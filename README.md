# tally

[![codecov](https://codecov.io/gh/wharflab/tally/graph/badge.svg?token=J3vK0hyLkf)](https://codecov.io/gh/wharflab/tally)

tally is a production-grade **Dockerfile/Containerfile linter + formatter** that keeps build files clean, modern, and consistent.

It uses **BuildKit's official parser and checks** (the same foundation behind `docker buildx`) plus a **safe auto-fix** engine. It runs fast,
doesn't require Docker Desktop or a daemon, and fits neatly into CI.

```bash
# Lint everything in the repo (recursive)
tally lint .

# Apply all safe fixes automatically
tally lint --fix Dockerfile
```

## Why tally

Modern Dockerfiles deserve modern tooling. tally is opinionated in the right places:

- **BuildKit-native**: understands modern syntax like heredocs, `RUN --mount=...`, `COPY --link`, and `ADD --checksum=...`.
- **Fixes, not just findings**: `--fix` applies safe, mechanical rewrites; `--fix-unsafe` unlocks opt-in risky fixes (including AI).
- **Modernizes on purpose**: converts eligible `RUN`/`COPY` instructions to heredocs, prefers `ADD --extract`, and more.
- **Broad rule coverage**: combines Docker's official BuildKit checks, Hadolint-compatible rules, and tally-specific rules.
- **Registry-aware without Docker**: uses a Podman-compatible registry client for image metadata checks (no daemon required).
- **Editor + CI friendly**: VS Code extension (`wharflab.tally`, powered by `tally lsp`) and outputs for JSON, SARIF, and GitHub Actions annotations.
- **Easy to install anywhere**: Homebrew, Go, npm, pip, and RubyGems.
- **Written in Go**: single fast binary, built on production-grade libraries.

Quality bar: **92% code coverage on Codecov** and **2,900+ Go tests executed in CI**.

Dockerfile linting usually means picking a compromise:

- **Hadolint** is popular and battle-tested, but it uses its own Dockerfile parser, so support for newer BuildKit features can lag behind. It also
  is commonly consumed as a prebuilt binary, and it focuses on reporting — not fixing.
- **`docker buildx --check`** runs Docker's official BuildKit checks, but it requires the Docker/buildx toolchain and can be heavier than a pure
  static linter (and not always available if you're using Podman/Finch/other runtimes).

Roadmap: more auto-fixes, more Hadolint parity, richer registry-aware checks, and higher-level rules (cache & tmpfs mount recommendations,
tooling-aware checks for uv/bun, line-length and layer optimizations).

## Optional: AI AutoFix via ACP

tally supports **opt-in AI AutoFix** for the kinds of improvements that are hard to express as a deterministic rewrite.

Instead of asking you for an API key, tally integrates with **ACP (Agent Client Protocol)** so you can use the agent you already trust (Gemini CLI,
OpenCode, GitHub Copilot CLI, and more), while tally keeps linting fast and validates proposed changes before applying them.

AI fixes are **rule-driven** (one narrow transformation at a time) and **verified** (re-parse + re-lint) before anything is applied.

- Guide: [`docs/guide/ai-autofix-acp.md`](docs/guide/ai-autofix-acp.md)

## Supported Rules

tally integrates rules from multiple sources:

<!-- BEGIN RULES_TABLE -->
| Source | Rules | Description |
|--------|-------|-------------|
| **[BuildKit](https://docs.docker.com/reference/build-checks/)** | 22/22 rules | Docker's official Dockerfile checks (captured + reimplemented) |
| **tally** | 9 rules | Custom rules including secret detection with [gitleaks](https://github.com/gitleaks/gitleaks) |
| **[Hadolint](https://github.com/hadolint/hadolint)** | 37 rules | Hadolint-compatible Dockerfile rules (expanding) |
<!-- END RULES_TABLE -->

**See [RULES.md](RULES.md) for the complete rules reference.**

## Installation

### Homebrew (macOS/Linux)

```bash
brew install wharflab/tap/tally
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
go install github.com/wharflab/tally@latest
```

### From Source

```bash
git clone https://github.com/wharflab/tally.git
cd tally
go build .
```

## Usage

```bash
# Check a Dockerfile
tally lint Dockerfile

# Check all Dockerfiles in current directory (recursive)
tally lint .

# Check with glob patterns
tally lint "**/*.Dockerfile"

# Exclude patterns
tally lint --exclude "vendor/*" --exclude "test/*" .

# Check with max lines limit
tally lint --max-lines 100 Dockerfile

# Output as JSON
tally lint --format json Dockerfile

# Check multiple files
tally lint Dockerfile.dev Dockerfile.prod

# Enable context-aware rules (e.g., copy-ignored-file)
tally lint --context . Dockerfile
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
tally lint --exclude "vendor/*" --exclude "test/*" .

# Exclude all .bak files
tally lint --exclude "*.bak" .
```

## Rules Overview

For the complete list of all supported rules, see **[RULES.md](RULES.md)**.

### Context-Aware Rules

Some rules require build context awareness. Enable them with the `--context` flag:

```bash
# Enable context-aware rules
tally lint --context . Dockerfile
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

Suppress specific violations using inline comment directives:

```dockerfile
# tally ignore=StageNameCasing
FROM alpine AS Build

# tally global ignore=max-lines;reason=Generated file
FROM alpine
```

tally also supports `hadolint` and `check=skip` directive formats for easy migration.

**See [Configuration Guide](docs/guide/configuration.md#inline-directives) for full directive syntax.**

## Configuration

Create a `.tally.toml` in your project:

```toml
[output]
format = "text"
fail-level = "warning"

[rules]
include = ["buildkit/*", "tally/*"]
exclude = ["buildkit/MaintainerDeprecated"]

[rules.tally.max-lines]
max = 100
```

Configuration priority: CLI flags > environment variables > config file > defaults.

**See [Configuration Guide](docs/guide/configuration.md) for full reference.**

## Output Formats

tally supports multiple output formats for different use cases.

### Text (default)

Human-readable output with colors and source code snippets:

```bash
tally lint Dockerfile
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
tally lint --format json Dockerfile
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
  "rules_enabled": 41
}
```

### SARIF

[Static Analysis Results Interchange Format](https://docs.oasis-open.org/sarif/sarif/v2.1.0/) for CI/CD integration with GitHub Code Scanning, Azure
DevOps, and other tools:

```bash
tally lint --format sarif Dockerfile > results.sarif
```

### GitHub Actions

Native GitHub Actions workflow command format for inline annotations:

```bash
tally lint --format github-actions Dockerfile
```

```text
::warning file=Dockerfile,line=2,title=StageNameCasing::Stage name 'Builder' should be lowercase
```

### Markdown

Concise Markdown tables optimized for AI agents and token efficiency:

```bash
tally lint --format markdown Dockerfile
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
tally lint --fail-level error Dockerfile

# Never fail (useful for CI reporting without blocking)
tally lint --fail-level none --format sarif Dockerfile > results.sarif

# Fail on any violation including style issues (default behavior)
tally lint --fail-level style Dockerfile
```

Available levels (from most to least severe): `error`, `warning`, `info`, `style` (default), `none`

## IDE Integration

### VS Code

Install the official **[tally extension](https://marketplace.visualstudio.com/items?itemName=wharflab.tally)** from the Visual Studio Marketplace for real-time linting and diagnostics.

### JetBrains IDEs

Install the official **[Tally plugin](https://plugins.jetbrains.com/plugin/30255-tally)** from JetBrains Marketplace for IDE integration in IntelliJ-based editors.

### Other Editors

Any editor that supports the [Language Server Protocol](https://microsoft.github.io/language-server-protocol/) can use tally's built-in LSP server over stdio:

```bash
npx -y tally-cli lsp --stdio
```

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
