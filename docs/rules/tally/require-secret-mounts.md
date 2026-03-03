# tally/require-secret-mounts

Enforces `--mount=type=secret` on RUN instructions that execute commands requiring access to secrets — private registry credentials, API keys, cloud
provider tokens, and similar sensitive data.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Security |
| Default | Off (requires user configuration) |
| Auto-fix | Yes (`--fix`) |

## Description

BuildKit secret mounts (`--mount=type=secret`) are the recommended way to pass sensitive data into build steps without baking it into the image layer.
Without enforcement it is easy to forget the mount flag, causing builds to fail or — worse — fall back to unauthenticated access silently.

This rule lets you declare which commands need which secrets and enforces the declaration at lint time. Secrets can be mounted as **files** (via
`target`) or as **environment variables** (via `env`).

The rule is **disabled by default** and requires explicit user configuration mapping command names to secret mount specifications.

## Configuration

Map each command name to a secret mount specification:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | **Required.** Secret ID for the `--mount` flag. |
| `target` | string | File path where the secret is mounted inside the container. |
| `env` | string | Environment variable name to expose the secret as. |
| `required` | bool | Fail the build if the secret is not provided (default: `false`). |

At least one of `target` or `env` must be set. Both can be used together — Docker supports mounting a secret as both a file and an environment
variable simultaneously.

```toml
[rules.tally.require-secret-mounts]
severity = "warning"

# File-based secret (mounted as a file inside the container)
[rules.tally.require-secret-mounts.commands.pip]
id = "pipconf"
target = "/root/.config/pip/pip.conf"
required = true

# Environment-variable secret (exposed as an env var)
[rules.tally.require-secret-mounts.commands.gh]
id = "gh-token"
env = "GH_TOKEN"

# Both file and env var (Docker supports this)
[rules.tally.require-secret-mounts.commands.aws]
id = "aws-creds"
target = "/root/.aws/credentials"
env = "AWS_SHARED_CREDENTIALS_FILE"
required = true
```

## Examples

### Private package registry (pip + AWS CodeArtifact)

```toml
[rules.tally.require-secret-mounts.commands.pip]
id = "pipconf"
target = "/root/.config/pip/pip.conf"
```

Before:

```dockerfile
FROM python:3.12-slim
RUN pip install -r requirements.txt
```

After `--fix`:

```dockerfile
FROM python:3.12-slim
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf pip install -r requirements.txt
```

Build command:

```bash
docker build --secret id=pipconf,src=$HOME/.config/pip/pip.conf .
```

### AWS CLI with credentials file

```toml
[rules.tally.require-secret-mounts.commands.aws]
id = "aws"
target = "/root/.aws/credentials"
```

Before:

```dockerfile
FROM amazon/aws-cli:latest
RUN aws s3 cp s3://my-bucket/data.tar.gz /app/
```

After `--fix`:

```dockerfile
FROM amazon/aws-cli:latest
RUN --mount=type=secret,id=aws,target=/root/.aws/credentials aws s3 cp s3://my-bucket/data.tar.gz /app/
```

### GitHub CLI with token via environment variable

```toml
[rules.tally.require-secret-mounts.commands.gh]
id = "gh-token"
env = "GH_TOKEN"
```

Before:

```dockerfile
FROM alpine:3.21
RUN gh auth login && gh extension install github/gh-copilot
```

After `--fix`:

```dockerfile
FROM alpine:3.21
RUN --mount=type=secret,id=gh-token,env=GH_TOKEN gh auth login && gh extension install github/gh-copilot
```

Build command:

```bash
docker build --secret id=gh-token,env=GH_TOKEN .
```

### Existing mounts are preserved

If a `RUN` already has other mounts (e.g., cache mounts), the fix inserts secret mounts without touching the rest of the instruction:

```dockerfile
# Before
RUN --mount=type=cache,target=/root/.cache/pip pip install -r requirements.txt

# After --fix
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf --mount=type=cache,target=/root/.cache/pip pip install -r requirements.txt
```

## Cross-Rule Interaction

This rule works alongside `tally/prefer-package-cache-mounts`. Both rules can fire on the same `RUN` instruction. Both use zero-length insertions
right after `RUN` for their mount flags, so they compose in a single `--fix` pass without conflicting.

## References

- [Docker Build Secrets](https://docs.docker.com/build/building/secrets/) — official Docker documentation on using secret mounts
