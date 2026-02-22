# buildkit/InvalidBaseImagePlatform

Validates that the platform of an external base image matches the expected target platform.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled (requires `--slow-checks` for async resolution) |

## Description

When using `--platform` or `$TARGETPLATFORM`, this rule checks that the base
image actually supports the requested platform by resolving image metadata from
the registry.

This is an async rule that runs with `--slow-checks`, as it requires resolving
image metadata from the registry.

## Examples

Bad (image not available for requested platform):

```dockerfile
FROM --platform=linux/s390x ubuntu:22.04
# Error if ubuntu:22.04 is not available for linux/s390x
```

Good:

```dockerfile
FROM --platform=linux/amd64 ubuntu:22.04
```

The error message includes available platforms:

```text
image "ubuntu:22.04" is not available on platform "linux/s390x" (available: [linux/amd64, linux/arm64])
```
