# tally/prefer-wget-config

Stages using wget should include a retry config to handle transient failures.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Reliability |
| Default | Enabled |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

Detects Dockerfile stages that use `wget` without a retry configuration file. This applies
both when `wget` is invoked directly in a `RUN` command and when the stage installs the
`wget` package first. Transient download failures are common during image builds, so a small
`wgetrc` file makes those stages more resilient.

The rule emits at most **one violation per stage** and triggers when:

- A `RUN` instruction invokes `wget` directly (for example `wget https://...`)
- A `RUN` instruction installs the `wget` package (for example `apt-get install -y wget`)
- On Windows: `wget.exe` invocation or package installs that resolve to `wget`

## Auto-fix

The fix inserts a short documentation comment plus two instructions before the first relevant
`RUN`:

- **Install trigger** (`apt-get install wget`): inserts right before the install `RUN`
- **Invocation trigger** (`wget https://...`): inserts before the first `RUN` in the stage
  when `wget` is already available from the base image

### Linux

```dockerfile
# [tally] wget configuration for improved robustness
ENV WGETRC=/etc/wgetrc
COPY --chmod=0644 <<EOF ${WGETRC}
retry_connrefused = on
timeout = 15
tries = 5
EOF
```

### Windows

```dockerfile
# [tally] wget configuration for improved robustness
ENV WGETRC=c:\wgetrc
COPY <<EOF ${WGETRC}
retry_connrefused = on
timeout = 15
tries = 5
EOF
```

On Windows, `--chmod` is omitted since it has no effect.

### Config options

- `retry_connrefused = on` retries connection-refused failures
- `timeout` limits how long each request can wait (default: 15 seconds)
- `tries` controls how many attempts wget makes (default: 5)

## Configuration

The emitted defaults can be overridden via rule config:

```toml
[rules.tally.prefer-wget-config]
timeout = 10
tries = 3
```

Supported startup-file directives are documented in the official GNU Wget manual:
[`Wgetrc Commands`](https://www.gnu.org/software/wget/manual/html_node/Wgetrc-Commands.html).

## Examples

### Before (violation)

```dockerfile
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y ca-certificates wget
RUN wget https://example.com/tool.tar.gz -O /tmp/tool.tar.gz
```

### After (fixed with --fix --fix-unsafe)

```dockerfile
FROM ubuntu:22.04
# [tally] wget configuration for improved robustness
ENV WGETRC=/etc/wgetrc
COPY --chmod=0644 <<EOF ${WGETRC}
retry_connrefused = on
timeout = 15
tries = 5
EOF
RUN apt-get update && apt-get install -y ca-certificates wget
RUN wget https://example.com/tool.tar.gz -O /tmp/tool.tar.gz
```

## Suppression

The rule does **not** trigger when:

- The config file `/etc/wgetrc`, `c:\wgetrc`, or a user-level `.wgetrc` already exists in
  the stage (via `COPY` heredoc, `COPY` from build context, `COPY --from` another stage, or
  `RUN` file creation)
- The `WGETRC` environment variable is already set in the stage

Child stages inheriting from a parent stage that already has the config also do not trigger.

## Related rules

- [`hadolint/DL3047`](../hadolint/DL3047.md) checks `wget` command progress output
- [`tally/prefer-add-unpack`](./prefer-add-unpack.md) rewrites `wget` download-and-extract
  patterns to `ADD --unpack`
- [`tally/prefer-copy-heredoc`](./prefer-copy-heredoc.md) detects file creation via `RUN`
  and suggests `COPY` heredoc instead
