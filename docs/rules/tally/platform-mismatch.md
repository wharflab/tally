# tally/platform-mismatch

Explicit `--platform` on FROM does not match what the registry provides.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |
| Requires | `--slow-checks=on` (registry queries) |

## Description

When a `FROM` instruction uses an explicit `--platform` flag, this rule queries the
container registry to verify that the requested platform is actually available for the
specified image. This catches provable mismatches before they fail at build time.

Unlike `buildkit/InvalidBaseImagePlatform`, this rule:

- **Only fires when `--platform` is explicitly set** on the `FROM` instruction
- **Never compares against the host platform**, so results are deterministic across machines
- **Skips automatic build args** (`$BUILDPLATFORM`, `$TARGETPLATFORM`, etc.) which are dynamic

## When it fires

| Scenario | Result |
|----------|--------|
| `FROM --platform=linux/arm64 image:tag` and registry has `linux/arm64` | No violation |
| `FROM --platform=linux/arm64 image:tag` and registry does NOT have `linux/arm64` | **Violation** |
| `FROM image:tag` (no `--platform`) | No violation |
| `FROM --platform=$BUILDPLATFORM image:tag` | No violation (dynamic) |
| `FROM --platform=$TARGETPLATFORM image:tag` | No violation (dynamic) |

## Examples

### Bad

```dockerfile
# python:3.12 is only published for linux/arm64 but linux/amd64 is requested
FROM --platform=linux/amd64 python:3.12
RUN pip install flask
```

### Good

```dockerfile
# No --platform: builder picks the right platform at build time
FROM python:3.12
RUN pip install flask
```

```dockerfile
# Correct platform that the image actually provides
FROM --platform=linux/arm64 python:3.12
RUN pip install flask
```

```dockerfile
# Dynamic platform via build arg
FROM --platform=$BUILDPLATFORM golang:1.22 AS builder
RUN go build -o /app
```

## Relationship to other rules

- **`buildkit/InvalidBaseImagePlatform`** (default: Off) — the BuildKit rule compares
  against the host platform even without `--platform`, producing non-deterministic results.
  This rule supersedes it with a stricter, deterministic approach.
- **`buildkit/FromPlatformFlagConstDisallowed`** (default: Off) — the BuildKit rule warns
  on any constant `--platform` value. This is too strict: hardcoded `--platform` is
  legitimate for ARM-only services, Windows containers, and cross-compilation. The new
  rule validates the platform against the registry instead of discouraging it.
