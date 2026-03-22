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

1. The COPY has a single source file (not a glob or multiple sources)
2. The COPY does not already use `--chmod`
3. The `RUN` is a standalone `chmod` command (not chained with other commands)
4. The chmod target matches the COPY destination

Both octal (`755`, `0755`) and symbolic (`+x`, `u+rwx`) chmod modes are supported.

## Examples

### Before (violation)

```dockerfile
FROM python:3.12-slim

COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

COPY --chown=appuser:appuser start.sh /usr/local/bin/start.sh
RUN chmod 755 /usr/local/bin/start.sh
```

### After (fixed with --fix)

```dockerfile
FROM python:3.12-slim

COPY --chmod=+x entrypoint.sh /app/entrypoint.sh

COPY --chmod=755 --chown=appuser:appuser start.sh /usr/local/bin/start.sh
```

## Auto-fix Conditions

The fix is emitted when:

- The COPY has exactly one source file
- The COPY does not already have `--chmod`
- The `RUN` is a standalone chmod (single command, not recursive)
- The chmod target is an absolute path matching the COPY destination

The fix preserves the original chmod notation (symbolic or octal) in the `--chmod` flag value.

## Limitations

- Only detects consecutive COPY + RUN chmod pairs (no intervening instructions)
- Skips COPY with glob patterns (`*.sh`) or multiple sources
- Skips `chmod -R` (recursive) since `--chmod` applies per-file
- Does not resolve relative chmod targets against WORKDIR
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
