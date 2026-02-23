# tally/require-stages

Dockerfile has no stages to build.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |
| Type | Fail-fast syntax check (exit code 4) |

## Description

Every Dockerfile must contain at least one `FROM` instruction to define a build stage.
A file with only `ARG`, `RUN`, `COPY`, or other instructions but no `FROM` always fails
at build time with:

```text
ERROR: dockerfile contains no stages to build
```

This check catches the problem before any other linting runs, aborting with exit code 4
(syntax error). Common causes:

- A `FROM` line was accidentally deleted during a refactor
- An AI-generated patch removed or failed to include a `FROM` instruction
- A Dockerfile fragment was saved as a standalone file

## Examples

### Bad

```dockerfile
# Missing FROM — no build stages defined
ARG VERSION=1.0
RUN apk add --no-cache curl
COPY . /app
```

### Good

```dockerfile
ARG VERSION=1.0
FROM alpine:3.20
RUN apk add --no-cache curl
COPY . /app
```

## Related Rules

- [`hadolint/DL3061`](../hadolint/DL3061.md) detects non-`ARG` instructions that appear
  *before* the first `FROM`. When no `FROM` exists at all, `tally/require-stages` fires
  first and DL3061 is not reached.
