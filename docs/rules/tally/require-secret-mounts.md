# tally/require-secret-mounts

Enforces `--mount=type=secret` for commands that access private registries or authenticated package sources.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Security |
| Default | Off (requires user configuration) |
| Auto-fix | Yes (`--fix`) |

## Description

Users who consume packages from private registries (e.g., AWS CodeArtifact, private npm, internal PyPI) need to mount secret configuration files into
their `RUN` instructions. Without enforcement, it is easy to forget the `--mount=type=secret` flag, causing builds to fail or fall back to public
registries silently.

This rule enforces that specific commands always have a matching `--mount=type=secret` with the correct `id` and `target`. It is
**disabled by default** and requires explicit user configuration mapping commands to secrets.

## Configuration

Map each command name to a `{id, target}` secret mount specification:

```toml
[rules.tally.require-secret-mounts]
severity = "warning"

[rules.tally.require-secret-mounts.commands.pip]
id = "pipconf"
target = "/root/.config/pip/pip.conf"

[rules.tally.require-secret-mounts.commands.npm]
id = "npmrc"
target = "/root/.npmrc"
```

The `id` value must match the `--mount=type=secret,id=...` argument, and the `target` must match the path where the secret file is mounted inside the
container.

## Examples

### Before (violation)

```dockerfile
FROM python:3.12-slim
RUN pip install -r requirements.txt
```

### After (fixed with --fix)

```dockerfile
FROM python:3.12-slim
RUN --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf pip install -r requirements.txt
```

### AWS CodeArtifact Example

For teams using AWS CodeArtifact as a private PyPI mirror:

```toml
[rules.tally.require-secret-mounts]
severity = "warning"

[rules.tally.require-secret-mounts.commands.pip]
id = "pipconf"
target = "/root/.config/pip/pip.conf"

[rules.tally.require-secret-mounts.commands.uv]
id = "pipconf"
target = "/root/.config/pip/pip.conf"
```

Build command:

```bash
docker build --secret id=pipconf,src=$HOME/.config/pip/pip.conf .
```

### Existing Mounts Are Preserved

If a `RUN` already has other mounts (e.g., cache mounts), the fix preserves them:

```dockerfile
# Before
RUN --mount=type=cache,target=/root/.cache/pip pip install -r requirements.txt

# After --fix
RUN --mount=type=cache,target=/root/.cache/pip --mount=type=secret,id=pipconf,target=/root/.config/pip/pip.conf pip install -r requirements.txt
```

## Cross-Rule Interaction

This rule works alongside `tally/prefer-package-cache-mounts`. Both rules can fire on the same `RUN` instruction. When running `--fix`, the secret
mount fix (priority 85) applies before the cache mount fix (priority 90). On the next pass, the cache mount rule fires again and adds cache mounts
while preserving the existing secret mount.
