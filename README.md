# tally

A fast, configurable linter for Dockerfiles and Containerfiles.

## Installation

### NPM

```bash
npm install -g @contino/tally
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

# Check with max lines limit
tally check --max-lines 100 Dockerfile

# Output as JSON
tally check --format json Dockerfile

# Check multiple files
tally check Dockerfile.dev Dockerfile.prod
```

## Available Rules

| Rule | Description | Options |
|------|-------------|---------|
| `max-lines` | Enforce maximum number of lines | `max`, `skip-blank-lines`, `skip-comments` |

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

| Flag | Description |
|------|-------------|
| `--no-inline-directives` | Disable processing of inline ignore directives |
| `--warn-unused-directives` | Warn about directives that don't suppress any violations |
| `--require-reason` | Warn about ignore directives without `reason=` explanation |

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
# Output format: "text" or "json"
format = "json"

[rules.max-lines]
max = 500
skip-blank-lines = true
skip-comments = true
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

| Variable | Description |
|----------|-------------|
| `TALLY_FORMAT` | Output format (`text` or `json`) |
| `TALLY_RULES_MAX_LINES_MAX` | Maximum lines allowed |
| `TALLY_RULES_MAX_LINES_SKIP_BLANK_LINES` | Exclude blank lines (`true`/`false`) |
| `TALLY_RULES_MAX_LINES_SKIP_COMMENTS` | Exclude comments (`true`/`false`) |
| `TALLY_NO_INLINE_DIRECTIVES` | Disable inline directive processing (`true`/`false`) |
| `TALLY_INLINE_DIRECTIVES_WARN_UNUSED` | Warn about unused directives (`true`/`false`) |
| `TALLY_INLINE_DIRECTIVES_REQUIRE_REASON` | Require reason= on ignore directives (`true`/`false`) |

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

### Text (default)

```
Dockerfile:0: file has 150 lines, maximum allowed is 100 (max-lines)
```

### JSON

```json
[
  {
    "file": "Dockerfile",
    "lines": 150,
    "issues": [
      {
        "rule": "max-lines",
        "line": 0,
        "message": "file has 150 lines, maximum allowed is 100",
        "severity": "error"
      }
    ]
  }
]
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

MIT
