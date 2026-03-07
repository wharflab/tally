# buildkit/FromPlatformFlagConstDisallowed

FROM `--platform` flag should not use a constant value.

| Property  | Value         |
|-----------|---------------|
| Severity  | Off           |
| Category  | Best Practice |
| Default   | Disabled (superseded by [`tally/platform-mismatch`](../tally/platform-mismatch.md)) |

## Tally behavior deviation

Tally disables this rule by default because hardcoding `--platform` is
legitimate in several real-world scenarios:

- **ARM-only services.** Deployments targeting AWS Graviton or other ARM-only
  infrastructure use `FROM --platform=linux/arm64` to ensure the correct
  architecture regardless of where the build runs.
- **Windows containers.** Windows Dockerfiles use
  `FROM --platform=windows/amd64 mcr.microsoft.com/...` to explicitly target
  Windows, which is necessary when the builder could be multi-platform.
- **Cross-compilation.** Go and Rust projects commonly use
  `FROM --platform=linux/amd64 golang:1.22` for a specific builder stage while
  the final image targets a different architecture.

Rather than discouraging constant `--platform` values, tally validates them
against the registry with [`tally/platform-mismatch`](../tally/platform-mismatch.md).
This catches provable errors (image doesn't publish the requested platform)
without flagging intentional platform pinning.

You can re-enable this rule via configuration if you prefer the BuildKit
behavior:

```yaml
rules:
  buildkit/FromPlatformFlagConstDisallowed:
    severity: warning
```

## Description

When the `--platform` flag appears with a hardcoded value, it restricts image
building to a single target platform, preventing multi-platform images.

The recommended strategy involves:

- Removing `FROM --platform` and applying `--platform` at build time.
- Substituting `$BUILDPLATFORM` or comparable variable expressions.
- Naming stages to reflect platform when containing platform-specific
  operations.

## Examples

Bad:

```dockerfile
FROM --platform=linux/amd64 alpine AS base
RUN apk add --no-cache git
```

Good (default platform):

```dockerfile
FROM alpine AS base
RUN apk add --no-cache git
```

Good (meta variable):

```dockerfile
FROM --platform=${BUILDPLATFORM} alpine AS base
RUN apk add --no-cache git
```

Good (multi-stage build with target architecture):

```dockerfile
FROM --platform=linux/amd64 alpine AS build_amd64
...

FROM --platform=linux/arm64 alpine AS build_arm64
...

FROM build_${TARGETARCH} AS build
...
```

## Supersedes

- [hadolint/DL3029](../hadolint/DL3029.md)

## See also

- [`tally/platform-mismatch`](../tally/platform-mismatch.md) — validates explicit `--platform` against the registry instead of discouraging it
- [buildkit/FromPlatformFlagConstDisallowed](https://docs.docker.com/reference/build-checks/from-platform-flag-const-disallowed/)
