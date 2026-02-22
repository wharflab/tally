# buildkit/FromPlatformFlagConstDisallowed

FROM `--platform` flag should not use a constant value.

| Property  | Value         |
|-----------|---------------|
| Severity  | Warning       |
| Category  | Best Practice |
| Default   | Enabled       |

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

## Reference

- [buildkit/FromPlatformFlagConstDisallowed](https://docs.docker.com/reference/build-checks/from-platform-flag-const-disallowed/)
