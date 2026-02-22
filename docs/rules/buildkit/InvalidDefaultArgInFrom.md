# buildkit/InvalidDefaultArgInFrom

Using the global ARGs with default values should produce a valid build.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |

## Description

An `ARG` used in an image reference should be valid when no build arguments are
used. An image build should not require `--build-arg` to produce a valid build.

If a global `ARG` has no default value and is interpolated into a `FROM`
instruction, the resulting image reference may be invalid when the argument is
not supplied at build time.

## Examples

Bad:

```dockerfile
ARG TAG
FROM busybox:${TAG}
```

Good:

```dockerfile
ARG TAG=latest
FROM busybox:${TAG}
```

Good (empty ARG is OK if image is valid with it empty):

```dockerfile
ARG VARIANT
FROM busybox:stable${VARIANT}
```

Good (default value syntax):

```dockerfile
ARG TAG
FROM alpine:${TAG:-3.14}
```

## Reference

- <https://docs.docker.com/reference/build-checks/invalid-default-arg-in-from/>
