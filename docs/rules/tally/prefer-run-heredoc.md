# tally/prefer-run-heredoc

Suggests using heredoc syntax for multi-command RUN instructions.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Off (experimental) |
| Auto-fix | Yes (`--fix`) |

## Description

Suggests converting multi-command RUN instructions to heredoc syntax for better readability.

This rule targets Dockerfile
[here-documents](https://docs.docker.com/reference/dockerfile/#here-documents) with `RUN`, which are supported by BuildKit syntax.

Detects two patterns:

1. **Multiple consecutive RUN instructions** that could be combined
2. **Single RUN with chained commands** via `&&` (3+ commands by default)

## Why heredoc?

Heredoc syntax for RUN instructions offers:

- **Readability**: Each command on its own line, no `&&` or `\` clutter
- **Maintainability**: Easy to add, remove, or reorder commands
- **Debugging**: Clear line numbers in error messages

## Examples

### Before (violation)

```dockerfile
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl tzdata && \
    rm -rf /var/lib/apt/lists/*
```

### After (fixed with --fix)

```dockerfile
RUN <<EOF
set -e
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl tzdata
rm -rf /var/lib/apt/lists/*
EOF
```

### Another real-life example

```dockerfile
RUN mkdir -p /app /var/log/myapp && \
    addgroup -S app && \
    adduser -S -G app app && \
    chown -R app:app /app /var/log/myapp
```

```dockerfile
RUN <<EOF
set -e
mkdir -p /app /var/log/myapp
addgroup -S app
adduser -S -G app app
chown -R app:app /app /var/log/myapp
EOF
```

## Why `set -e`?

Heredocs don't stop on error by default - only the exit code of the last command matters. Adding `set -e` preserves the fail-fast behavior of `&&`
chains.

See [moby/buildkit#2722](https://github.com/moby/buildkit/issues/2722) for details.

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `min-commands` | integer | 3 | Minimum commands to trigger (heredocs add 2 lines overhead) |
| `check-consecutive-runs` | boolean | true | Check for consecutive RUN instructions |
| `check-chained-commands` | boolean | true | Check for `&&` chains in single RUN |

## Configuration

```toml
[rules.tally.prefer-run-heredoc]
severity = "style"
min-commands = 3
check-consecutive-runs = true
check-chained-commands = true
```

## Rule Coordination

When this rule is enabled, `hadolint/DL3003` (cd → WORKDIR) will skip generating fixes for commands that are heredoc candidates, allowing heredoc
conversion to handle `cd` correctly within the script.

## References

- [Dockerfile here-documents](https://docs.docker.com/reference/dockerfile/#here-documents)
