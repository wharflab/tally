# tally/prefer-curl-config

Stages using curl should include a retry config to handle transient failures.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Reliability |
| Default | Enabled |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

Detects Dockerfile stages that use `curl` (either invoked directly in a `RUN` command or
installed as a package) without a retry configuration file. Transient download failures are
common during image builds — network timeouts, temporary server errors, and DNS hiccups can
cause builds to fail unpredictably. A small `.curlrc` file with retry settings makes builds
significantly more robust.

The rule emits at most **one violation per stage** and triggers when:

- A `RUN` instruction invokes `curl` directly (e.g., `curl -fsSL https://...`)
- A `RUN` instruction installs the `curl` package (e.g., `apt-get install -y curl`)
- On Windows: `curl.exe` invocation or `choco install curl` / `winget install curl`

## Auto-fix

The fix inserts a short documentation comment plus two instructions before the first relevant `RUN`:

- **Install trigger** (`apt-get install curl`): inserts right before the install `RUN`
- **Invocation trigger** (`curl https://...`): inserts before the first `RUN` in the stage
  (curl is already available from the base image)

### Linux

```dockerfile
# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
```

### Windows

```dockerfile
# [tally] curl configuration for improved robustness
ENV CURL_HOME=c:\curl
COPY <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
```

On Windows, `--chmod` is omitted since it has no effect.

### Config options

- `--retry-connrefused` retries on connection-refused errors
- `--connect-timeout` limits the connection phase (default: 15 seconds)
- `--retry` retries failed transfers (default: 5)
- `--max-time` limits the entire transfer (default: 300 seconds)

## Configuration

The emitted defaults can be overridden via rule config:

```toml
[rules.tally.prefer-curl-config]
retry = 3
connect-timeout = 10
max-time = 120
```

## Examples

### Before (violation)

```dockerfile
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y ca-certificates curl
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
RUN apt-get install -y nodejs
```

### After (fixed with --fix --fix-unsafe)

```dockerfile
FROM ubuntu:22.04
# [tally] curl configuration for improved robustness
ENV CURL_HOME=/etc/curl
COPY --chmod=0644 <<EOF ${CURL_HOME}/.curlrc
--retry-connrefused
--connect-timeout 15
--retry 5
--max-time 300
EOF
RUN apt-get update && apt-get install -y ca-certificates curl
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
RUN apt-get install -y nodejs
```

## Suppression

The rule does **not** trigger when:

- The config file `/etc/curl/.curlrc` (or `c:\curl\.curlrc` on Windows) already exists in
  the stage (via `COPY` heredoc, `COPY` from build context, `COPY --from` another stage,
  or `RUN` file creation)
- The `CURL_HOME` environment variable is already set in the stage

Child stages inheriting from a parent stage that already has the config also do not trigger.

## Related rules

- [`tally/curl-should-follow-redirects`](./curl-should-follow-redirects.md) — ensures curl
  uses `--location` to follow HTTP redirects
- [`tally/prefer-copy-heredoc`](./prefer-copy-heredoc.md) — detects file creation via `RUN`
  and suggests `COPY` heredoc instead
