# tally/syntax-directive-typo

Detects typos in `# syntax=` parser directives.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |
| Type | Fail-fast syntax check (exit code 4) |

## Description

The `# syntax=` directive at the top of a Dockerfile selects the BuildKit frontend
image used to parse and build the file. A typo in this directive (e.g.,
`docker/dokcerfile` instead of `docker/dockerfile`) causes the build to fail
immediately because the frontend image cannot be resolved.

This check validates the directive value against well-known frontends
(`docker/dockerfile`, `docker.io/docker/dockerfile`) and suggests corrections
when the value is within a small edit distance. It also flags directives that
contain whitespace, which is never valid.

This is a fail-fast check: if a typo is detected, linting aborts with exit code 4.

## Examples

### Bad

```dockerfile
# syntax=docker/dokcerfile:1.7
FROM alpine:3.20
RUN echo "hello"
```

Output:

```text
Error: Dockerfile:1: syntax directive "docker/dokcerfile:1.7" looks misspelled (did you mean "docker/dockerfile:1.7"?)
```

### Good

```dockerfile
# syntax=docker/dockerfile:1.7
FROM alpine:3.20
RUN echo "hello"
```
