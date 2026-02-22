# tally/invalid-onbuild-trigger

ONBUILD trigger instruction is not a valid Dockerfile instruction.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | Yes (suggestion, requires `--fix-unsafe`) |

## Description

`ONBUILD` allows a base image to embed instructions that run automatically when the
image is used as a base for another build. However, the trigger instruction must be
a valid Dockerfile keyword.

Typo trigger keywords (e.g. `ONBUILD COPPY . /app`) successfully parse at the AST
level but fail at build time. The outer `ONBUILD` keyword is valid, so the
top-level unknown-instruction check does not catch these. This rule checks the
trigger keyword itself.

When the unknown trigger closely resembles a valid instruction (Levenshtein distance ≤ 2),
tally proposes a correction. The fix is classified as `suggestion` because it is
based on edit-distance inference — review it before applying.

Forbidden triggers (`FROM`, `ONBUILD`, `MAINTAINER`) are excluded from this rule;
they are already caught by [`hadolint/DL3043`](../hadolint/DL3043.md).

## References

- [Dockerfile reference — ONBUILD limitations](https://docs.docker.com/reference/dockerfile/#onbuild-limitations)
- [`hadolint/DL3043`](../hadolint/DL3043.md) — forbidden instructions as ONBUILD triggers

## Examples

### Bad

```dockerfile
FROM alpine:3.19

# COPPY is not a valid instruction — should be COPY
ONBUILD COPPY . /app

# RUNN is not a valid instruction — should be RUN
ONBUILD RUNN apk add --no-cache ca-certificates
```

### Good

```dockerfile
FROM alpine:3.19
ONBUILD COPY . /app
ONBUILD RUN apk add --no-cache ca-certificates
```

## Configuration

```toml
[rules.tally.invalid-onbuild-trigger]
severity = "error"  # Options: "off", "error", "warning", "info", "style"
```
