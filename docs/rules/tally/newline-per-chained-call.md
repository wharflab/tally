# tally/newline-per-chained-call

Each chained element within an instruction should be on its own line.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Enabled |
| Auto-fix | Yes (safe) |

## Description

Enforces that chained elements within Dockerfile instructions are placed on separate continuation lines using `\`. This improves readability and
produces cleaner diffs.

Applies to three instruction types:

- **RUN** -- splits `&&`/`||` chain boundaries AND splits multiple `--mount=` flags
- **LABEL** -- splits multiple `key=value` pairs
- **HEALTHCHECK CMD** -- splits `&&`/`||` chain boundaries (shell form only)

## Examples

### Bad

```dockerfile
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

RUN --mount=type=cache,target=/var/cache/apt --mount=type=bind,source=go.sum,target=go.sum apt-get update

LABEL org.opencontainers.image.title=myapp org.opencontainers.image.version=1.0 org.opencontainers.image.vendor=acme

HEALTHCHECK CMD curl -f http://localhost/ && wget -qO- http://localhost/health || exit 1
```

### Good

```dockerfile
RUN apt-get update \
	&& apt-get install -y curl \
	&& rm -rf /var/lib/apt/lists/*

RUN --mount=type=cache,target=/var/cache/apt \
	--mount=type=bind,source=go.sum,target=go.sum \
	apt-get update

LABEL org.opencontainers.image.title=myapp \
	org.opencontainers.image.version=1.0 \
	org.opencontainers.image.vendor=acme

HEALTHCHECK CMD curl -f http://localhost/ \
	&& wget -qO- http://localhost/health \
	|| exit 1
```

## Configuration

Default (no config needed):

```toml
# Enabled by default with min-commands = 2
```

Require 3+ chained commands before splitting:

```toml
[rules.tally.newline-per-chained-call]
min-commands = 3
```

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `min-commands` | integer | `2` | Minimum chained commands to trigger chain splitting (>= 2). Applies to RUN and HEALTHCHECK CMD. |

LABEL pair splitting and mount splitting always trigger when 2+ elements share a line (not affected by `min-commands`).

## Skipped Cases

The rule skips the following:

- **Exec form** (`RUN ["cmd"]`) -- no shell to parse
- **Heredoc RUN** (`RUN <<EOF`) -- chain splitting skipped; mount splitting still applies
- **Non-POSIX shell** (PowerShell, cmd) -- incompatible syntax
- **Inline heredocs** (`cat <<EOF && cmd`) -- reformatting would break heredoc boundaries
- **Single command** -- no chain to split
- **Already formatted** -- elements already on separate lines
- **Legacy LABEL format** (`LABEL key value`) -- handled by `buildkit/LegacyKeyValueFormat`
- **prefer-run-heredoc coordination** -- if `prefer-run-heredoc` is enabled and the command is a heredoc candidate, chain splitting is skipped to
  avoid conflicting fixes

## Auto-fix

This rule provides a safe auto-fix. Continuation lines use tab indentation consistent with shell conventions (`shfmt --bn`):

```bash
tally lint --fix Dockerfile
```

For combined violations (e.g., a RUN with both multiple mounts and chained commands), all edits are applied atomically in a single fix.
