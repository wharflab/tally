# tally/unknown-instruction

Detects misspelled or invalid Dockerfile instruction keywords.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |
| Type | Fail-fast syntax check (exit code 4) |

## Description

Dockerfile instructions must be one of the keywords recognized by BuildKit
(`FROM`, `RUN`, `COPY`, `ADD`, `WORKDIR`, `ENV`, `ARG`, `EXPOSE`, `LABEL`,
`CMD`, `ENTRYPOINT`, `VOLUME`, `USER`, `SHELL`, `HEALTHCHECK`, `ONBUILD`,
`STOPSIGNAL`, `MAINTAINER`).

A misspelled keyword (e.g., `FORM`, `COPPY`, `WROKDIR`) is silently treated as
a comment by the parser, making the resulting image incorrect without any
obvious error. This check catches typos early using Levenshtein distance and
suggests the closest valid instruction when within edit distance 2.

This is a fail-fast check: if any unknown instruction is found, linting aborts
with exit code 4.

## Examples

### Bad

```dockerfile
FORM alpine:3.20
RUN echo "hello"
```

Output:

```text
Error: Dockerfile:1: unknown instruction "FORM" (did you mean "FROM"?)
```

### Good

```dockerfile
FROM alpine:3.20
RUN echo "hello"
```
