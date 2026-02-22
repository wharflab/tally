# buildkit/UndefinedVar

Usage of undefined variable.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |

## Description

This check ensures that environment variables and build arguments are correctly
declared before being used. While undeclared variables might not cause an
immediate build failure, they can lead to unexpected behavior. It also detects
common mistakes like typos in variable names.

This check does not evaluate undefined variables for `RUN`, `CMD`, and
`ENTRYPOINT` instructions where you use the shell form, because when you use
shell form, variables are resolved by the command shell.

## Examples

Bad:

```dockerfile
FROM alpine AS base
COPY $foo .
```

Good:

```dockerfile
FROM alpine AS base
ARG foo
COPY $foo .
```

Bad (typo detection):

```dockerfile
FROM alpine
ENV PATH=$PAHT:/app/bin
```

Output: `Usage of undefined variable '$PAHT' (did you mean $PATH?)`

## Supersedes

- [hadolint/DL3044](../hadolint/DL3044.md)

## Reference

- [buildkit/UndefinedVar](https://docs.docker.com/reference/build-checks/undefined-var/)
