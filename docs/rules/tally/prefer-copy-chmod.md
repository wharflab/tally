# tally/prefer-copy-chmod

Prefer `COPY --chmod` over a separate `COPY` followed by `RUN chmod`.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Style |
| Default | Enabled |
| Auto-fix | Yes (`--fix`) |

## Description

Detects a `COPY` instruction immediately followed by a `RUN chmod` that targets the same file, and suggests merging them into a single `COPY --chmod`
instruction.

The [`--chmod` flag](https://docs.docker.com/reference/dockerfile/#copy---chmod) sets file permissions at copy time, eliminating an extra layer and
the overhead of running a shell container just to change permissions.

## Why use COPY --chmod?

- **Fewer layers**: Merging two instructions into one reduces image layer count
- **Performance**: `COPY --chmod` sets permissions without spawning a shell container
- **Readability**: A single instruction is cleaner and easier to understand

## Detected Patterns

The rule flags consecutive `COPY` + `RUN chmod` pairs where:

1. The COPY has a single source file, heredoc, or single-dest content (not a glob or multiple sources)
2. The `RUN` is a standalone `chmod` command (shell-form or exec-form, not chained with other commands)
3. The chmod target matches the COPY effective destination (resolved against `WORKDIR` for relative paths)

Both octal (`755`, `0755`) and symbolic (`+x`, `u+rwx`, `-x`) chmod modes are supported.

### Merging with existing `--chmod`

When the COPY already has `--chmod`, the rule still fires if a `RUN chmod` follows:

- **Symbolic overlay**: `COPY --chmod=644` + `RUN chmod +x` merges to `COPY --chmod=0755`
- **Octal override**: `COPY --chmod=644` + `RUN chmod 755` merges to `COPY --chmod=755`
- **Redundant chmod**: `COPY --chmod=777` + `RUN chmod +x` flags the useless RUN (777 already includes execute)

## Examples

### Before (violation)

```dockerfile
FROM python:3.12-slim

COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

COPY --chown=appuser:appuser start.sh /usr/local/bin/start.sh
RUN chmod 755 /usr/local/bin/start.sh

COPY --chmod=644 config.sh /app/config.sh
RUN chmod +x /app/config.sh
```

### After (fixed with --fix)

```dockerfile
FROM python:3.12-slim

COPY --chmod=+x entrypoint.sh /app/entrypoint.sh

COPY --chmod=755 --chown=appuser:appuser start.sh /usr/local/bin/start.sh

COPY --chmod=0755 config.sh /app/config.sh
```

## Auto-fix Conditions

The fix is emitted when:

- The COPY has a single source file or heredoc content
- The `RUN` is a standalone chmod (single command, not recursive, shell-form or exec-form)
- The chmod target matches the COPY destination (absolute path or resolved via `WORKDIR`)

The fix preserves the original chmod notation (symbolic or octal) in the `--chmod` flag value.
When merging with an existing `--chmod`, the result is formatted as octal.

## Cross-Rule Interactions

- **`tally/prefer-copy-heredoc`**: Converts `RUN echo > file` to `COPY <<EOF`. No overlap — this rule only acts on `COPY` instructions, not `RUN` file
  creation. Both rules use the same fix priority (99) to avoid position drift when applied together.

## Limitations

- Only detects consecutive COPY + RUN chmod pairs (no intervening instructions)
- Skips COPY with glob patterns (`*.sh`) or multiple file sources
- Skips `chmod -R` (recursive) since `--chmod` applies per-file
- Does not detect ADD + RUN chmod patterns (only COPY)

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | true | Enable or disable the rule |

## Configuration

```toml
[rules.tally.prefer-copy-chmod]
enabled = true
```

## References

- [Dockerfile `COPY --chmod` reference](https://docs.docker.com/reference/dockerfile/#copy---chmod)
- [Dockerfile `COPY` reference](https://docs.docker.com/reference/dockerfile/#copy)
