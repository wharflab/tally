# buildkit/MultipleInstructionsDisallowed

Multiple CMD instructions should not be used in the same stage because only the last one will be used.

| Property  | Value         |
|-----------|---------------|
| Severity  | Warning       |
| Category  | Best Practice |
| Default   | Enabled       |
| Auto-fix  | Yes (`--fix`) |

## Description

If you have multiple `CMD`, `HEALTHCHECK`, or `ENTRYPOINT` instructions in your
Dockerfile, only the last occurrence is used. An image can only ever have one
`CMD`, one `HEALTHCHECK`, and one `ENTRYPOINT`.

## Examples

Bad:

```dockerfile
FROM alpine
ENTRYPOINT ["echo", "Hello, Norway!"]
ENTRYPOINT ["echo", "Hello, Sweden!"]
# Only "Hello, Sweden!" will be printed
```

Good:

```dockerfile
FROM alpine
ENTRYPOINT ["echo", "Hello, Norway!\nHello, Sweden!"]
```

You can have both a regular `CMD` and a separate `CMD` for `HEALTHCHECK`:

```dockerfile
FROM python:alpine
RUN apk add curl
HEALTHCHECK --interval=1s --timeout=3s \
  CMD ["curl", "-f", "http://localhost:8080"]
CMD ["python", "-m", "http.server", "8080"]
```

## Auto-fix

The fix comments out duplicate `CMD`/`ENTRYPOINT`/`HEALTHCHECK` instructions,
keeping only the last one in each stage.

```dockerfile
# Before
CMD echo "first"
CMD echo "second"

# After (with --fix)
# [commented out by tally - Docker will ignore all but last CMD]: CMD echo "first"
CMD echo "second"
```

## Supersedes

- [hadolint/DL3012](../hadolint/DL3012.md) (multiple `HEALTHCHECK`)
- [hadolint/DL4003](../hadolint/DL4003.md) (multiple `CMD`)
- [hadolint/DL4004](../hadolint/DL4004.md) (multiple `ENTRYPOINT`)

## Reference

- [buildkit/MultipleInstructionsDisallowed](https://docs.docker.com/reference/build-checks/multiple-instructions-disallowed/)
