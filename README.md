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

## Contributing

See [CLAUDE.md](CLAUDE.md) for development guidelines.

## License

MIT
