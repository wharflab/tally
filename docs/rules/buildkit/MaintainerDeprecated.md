# buildkit/MaintainerDeprecated

MAINTAINER instruction is deprecated in favor of using label.

| Property  | Value         |
|-----------|---------------|
| Severity  | Warning       |
| Category  | Best Practice |
| Default   | Enabled       |
| Auto-fix  | Yes (`--fix`) |

## Description

The `MAINTAINER` instruction, used historically for specifying the author of
the Dockerfile, is deprecated. To set author metadata for an image, use the
`org.opencontainers.image.authors` OCI label instead.

## Examples

Bad:

```dockerfile
MAINTAINER moby@example.com
```

Good:

```dockerfile
LABEL org.opencontainers.image.authors="moby@example.com"
```

## Auto-fix

The fix replaces `MAINTAINER` with an equivalent
`org.opencontainers.image.authors` label.

```dockerfile
# Before
MAINTAINER John Doe <john@example.com>

# After (with --fix)
LABEL org.opencontainers.image.authors="John Doe <john@example.com>"
```

## Supersedes

- [hadolint/DL4000](../hadolint/DL4000.md)

## Reference

- <https://docs.docker.com/reference/build-checks/maintainer-deprecated/>
