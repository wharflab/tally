# tally/prefer-package-cache-mounts

Suggests using BuildKit cache mounts for package-manager install/build commands.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Performance |
| Default | Off (experimental) |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

Flags `RUN` instructions that install dependencies or build artifacts with package managers but do not use cache mounts.

The rule follows Docker's official cache-mount guidance in the **Use cache mounts** section:

- <https://docs.docker.com/build/cache/optimize/#use-cache-mounts>

It also supports `uv` and `bun` package install flows.

## Detected Commands and Cache Targets

| Command pattern | Cache mount target(s) |
|-----------------|-----------------------|
| `npm install`, `npm ci`, `npm i` | `/root/.npm` |
| `go build`, `go mod download` | `/go/pkg/mod`, `/root/.cache/go-build` |
| `apt`/`apt-get` package operations | `/var/cache/apt` and `/var/lib/apt` (`sharing=locked`) |
| `dnf` package operations | `/var/cache/dnf` (`sharing=locked`) |
| `yum` package operations | `/var/cache/yum` (`sharing=locked`) |
| `pip install` | `/root/.cache/pip` |
| `bundle install` | `/root/.gem` |
| `cargo build` | `<WORKDIR>/target`, `/usr/local/cargo/git/db`, `/usr/local/cargo/registry` |
| `dotnet restore` | `/root/.nuget/packages` |
| `composer install` | `/tmp/cache` |
| `uv sync`, `uv pip install`, `uv tool install` | `/root/.cache/uv` |
| `bun install` | `/root/.bun/install/cache` |

## Examples

### Before (violation)

```dockerfile
FROM ubuntu:24.04
RUN --mount=type=secret,id=aptcfg,target=/etc/apt/auth.conf \
    apt-get update && apt-get install -y gcc && apt-get clean
```

### After (fixed with --fix --fix-unsafe)

```dockerfile
FROM ubuntu:24.04
RUN --mount=type=secret,id=aptcfg,target=/etc/apt/auth.conf \
    --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && apt-get install -y gcc
```

### Heredoc RUN support

```dockerfile
RUN <<EOF
npm install
npm cache clean --force
EOF
```

becomes:

```dockerfile
RUN --mount=type=cache,target=/root/.npm <<EOF
npm install
EOF
```

## What this rule removes (and why)

This cleanup only happens when the fix adds cache mounts for the related package manager.

The motivation is simple: these commands/flags either delete local package caches or explicitly disable caching, which cancels out the speed benefits
of cache mounts.

### Cache-cleaning commands removed

- **apt/apt-get**: `apt-get clean`, `apt clean`, and `rm -rf /var/lib/apt/lists*`
- **dnf**: `dnf clean ...` and `rm -rf /var/cache/dnf*`
- **yum**: `yum clean ...` and `rm -rf /var/cache/yum*`
- **npm**: `npm cache clean ...`
- **pip**: `pip cache purge`, `pip cache remove ...`
- **bundle**: `bundle clean ...`
- **dotnet**: `dotnet nuget locals ... --clear`
- **composer**: `composer clear-cache`, `composer clearcache`
- **uv**: `uv cache clean`, `uv cache prune`
- **bun**: `bun pm cache rm`, `bun pm cache clean`

### Cache-disabling flags removed

- **pip**: `--no-cache-dir`
- **uv**: `--no-cache`
- **bun**: `--no-cache`

## References

- [Docker cache optimization: Use cache mounts](https://docs.docker.com/build/cache/optimize/#use-cache-mounts)
- [Dockerfile `RUN --mount` reference](https://docs.docker.com/reference/dockerfile/#run---mount)
