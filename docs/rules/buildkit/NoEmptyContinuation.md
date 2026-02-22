# buildkit/NoEmptyContinuation

Empty continuation lines are deprecated and will cause errors in future Dockerfile syntax versions.

| Property  | Value       |
|-----------|-------------|
| Severity  | Error       |
| Category  | Correctness |
| Default   | Enabled     |
| Auto-fix  | Yes (`--fix`) |

## Description

Support for empty continuation (`\`) lines has been deprecated and will
generate errors in future versions of the Dockerfile syntax. Empty continuation
lines are empty lines following a newline escape.

## Examples

Bad:

```dockerfile
FROM alpine
EXPOSE \

80
```

Good:

```dockerfile
FROM alpine
EXPOSE \
# Port
80
```

Bad:

```dockerfile
FROM alpine
RUN apk add \

    gnupg \

    curl
```

Good (empty lines removed):

```dockerfile
FROM alpine
RUN apk add \
    gnupg \
    curl
```

## Auto-fix

The fix removes empty continuation lines from multi-line commands.

## Reference

- [buildkit/NoEmptyContinuation](https://docs.docker.com/reference/build-checks/no-empty-continuation/)
