# tally/no-trailing-spaces

Disallows trailing whitespace at the end of lines.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Enabled |
| Auto-fix | Yes (safe) |

## Description

Trailing whitespace (spaces and tabs at the end of lines) is invisible noise that clutters diffs in version control. This rule detects and can
automatically remove trailing whitespace from all lines in a Dockerfile.

## Examples

### Bad

```dockerfile
FROM alpine:3.20···
RUN echo hello··
COPY . /app→
```

(where `·` = space, `→` = tab)

### Good

```dockerfile
FROM alpine:3.20
RUN echo hello
COPY . /app
```

## Configuration

Default (no config needed):

```toml
# Enabled by default with no special options
```

Skip blank lines that consist entirely of whitespace:

```toml
[rules.tally.no-trailing-spaces]
skip-blank-lines = true
```

Ignore comment lines:

```toml
[rules.tally.no-trailing-spaces]
ignore-comments = true
```

Both options:

```toml
[rules.tally.no-trailing-spaces]
skip-blank-lines = true
ignore-comments = true
```

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `skip-blank-lines` | boolean | `false` | Skip lines that are entirely whitespace |
| `ignore-comments` | boolean | `false` | Skip any line starting with `#` (Dockerfile comments and `#` lines inside heredoc bodies) |

## Auto-fix

This rule provides a safe auto-fix that removes trailing whitespace from offending lines:

```bash
tally lint --fix Dockerfile
```

Each trailing whitespace occurrence is removed independently, so the fix count equals the number of affected lines.
