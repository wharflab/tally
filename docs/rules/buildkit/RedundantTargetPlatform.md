# buildkit/RedundantTargetPlatform

Setting platform to predefined `$TARGETPLATFORM` in FROM is redundant as this is the default behavior.

| Property  | Value         |
|-----------|---------------|
| Severity  | Info          |
| Category  | Best Practice |
| Default   | Enabled       |

## Description

A custom platform can be used for a base image. The default platform is the
same platform as the target output, so setting the platform to
`$TARGETPLATFORM` is redundant and unnecessary.

## Examples

Bad:

```dockerfile
FROM --platform=$TARGETPLATFORM alpine AS builder
RUN apk add --no-cache git
```

Good:

```dockerfile
FROM alpine AS builder
RUN apk add --no-cache git
```

## Reference

- <https://docs.docker.com/reference/build-checks/redundant-target-platform/>
