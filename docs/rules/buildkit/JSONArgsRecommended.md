# buildkit/JSONArgsRecommended

JSON arguments recommended for ENTRYPOINT/CMD to prevent unintended behavior related to OS signals.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Best Practice |
| Default | Enabled |
| Auto-fix | Yes (`--fix`) |

## Description

`ENTRYPOINT` and `CMD` instructions both support shell form and exec form.
When you use shell form, the executable runs as a child process to a shell,
which doesn't pass signals. This means that the program running in the
container can't detect OS signals like `SIGTERM` and `SIGKILL` and respond to
them correctly.

## Examples

Bad:

```dockerfile
FROM alpine
ENTRYPOINT my-program start
# entrypoint becomes: /bin/sh -c my-program start
```

Good:

```dockerfile
FROM alpine
ENTRYPOINT ["my-program", "start"]
# entrypoint becomes: my-program start
```

### Workarounds

If you need shell features (variable expansion, piping, command chaining), you
can:

1. Create a wrapper script:

```dockerfile
FROM alpine
RUN apk add bash
COPY --chmod=755 <<EOT /entrypoint.sh
#!/usr/bin/env bash
set -e
my-program start
EOT
ENTRYPOINT ["/entrypoint.sh"]
```

2. Explicitly specify the shell (suppresses the warning):

```dockerfile
FROM alpine
RUN apk add bash
SHELL ["/bin/bash", "-c"]
ENTRYPOINT echo "hello world"
```

## Auto-fix

Fix safety: `FixSuggestion` -- converts shell form to JSON array form.

Before:

```dockerfile
CMD echo hello world
```

After (with `--fix`):

```dockerfile
CMD ["echo", "hello", "world"]
```

## Related Rules

- [`tally/invalid-json-form`](../tally/invalid-json-form.md) -- detects instructions that
  *attempt* JSON exec-form but have invalid JSON (e.g., unquoted strings, single quotes).
  BuildKit silently falls back to shell-form for these, so both rules fire on the same
  instruction. tally's supersession processor suppresses the lower-severity
  `JSONArgsRecommended` (info) when `invalid-json-form` (error) is present at the same line.

## Supersedes

- [hadolint/DL3025](../hadolint/DL3025.md)

## Reference

- [buildkit/JSONArgsRecommended](https://docs.docker.com/reference/build-checks/json-args-recommended/)
