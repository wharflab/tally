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

- [Docker cache optimization](https://docs.docker.com/build/cache/optimize/#use-cache-mounts)

It also supports `pnpm`, `uv`, and `bun` package install flows.

Each suggested mount includes an `id` for observability and reusability across build stages.

## Detected Commands and Cache Targets

| Command pattern | Cache mount target(s) |
|-----------------|-----------------------|
| `npm install`, `npm ci`, `npm i` | `$npm_config_cache` or `/root/.npm` (`id=npm`) |
| `go build`, `go mod download` | `/go/pkg/mod` (`id=gomod`), `/root/.cache/go-build` (`id=gobuild`) |
| `apt`/`apt-get` package operations | `/var/cache/apt` (`id=apt`, `sharing=locked`) and `/var/lib/apt` (`id=aptlib`, `sharing=locked`) |
| `apk` package operations | `/var/cache/apk` (`id=apk`, `sharing=locked`) |
| `dnf` package operations | `/var/cache/dnf` (`id=dnf`, `sharing=locked`) |
| `yum` package operations | `/var/cache/yum` (`id=yum`, `sharing=locked`) |
| `zypper` package operations | `/var/cache/zypp` (`id=zypper`, `sharing=locked`) |
| `pip install` | `/root/.cache/pip` (`id=pip`) |
| `bundle install` | `/root/.gem` (`id=gem`) |
| `yarn install`, `yarn add` | `/usr/local/share/.cache/yarn` (`id=yarn`) |
| `pnpm install`, `pnpm add`, `pnpm i` | `$PNPM_HOME/store` or `/root/.pnpm-store` (`id=pnpm`) |
| `cargo build` | `<WORKDIR>/target` (`id=cargo-target`), `/usr/local/cargo/git/db` (`id=cargo-git`), `/usr/local/cargo/registry` (`id=cargo-registry`) |
| `dotnet restore` | `/root/.nuget/packages` (`id=nuget`) |
| `composer install` | `/root/.cache/composer` (`id=composer`) |
| `uv sync`, `uv pip install`, `uv tool install`, `uv python install` | `/root/.cache/uv` (`id=uv`) |
| `bun install` | `$BUN_INSTALL_CACHE_DIR` or `/root/.bun/install/cache` (`id=bun`) |

### Cache path resolution from environment variables

The rule resolves custom cache paths from `ENV` instructions in the Dockerfile:

| ENV variable | Mount ID | Resolution |
|---|---|---|
| `npm_config_cache` (case insensitive) | `npm` | Uses value directly (default: `/root/.npm`) |
| `PNPM_HOME` | `pnpm` | Appends `/store` to value (default: `/root/.pnpm-store`) |
| `BUN_INSTALL_CACHE_DIR` | `bun` | Uses value directly (default: `/root/.bun/install/cache`) |

If the variable value contains `$` (unresolved shell reference), the override is skipped.

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
    --mount=type=cache,target=/var/cache/apt,id=apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,id=aptlib,sharing=locked \
    apt-get update && apt-get install -y gcc
```

### pnpm with PNPM_HOME

```dockerfile
FROM node:20-slim
ENV PNPM_HOME="/pnpm"
RUN pnpm install --frozen-lockfile && pnpm store prune
```

becomes:

```dockerfile
FROM node:20-slim
ENV PNPM_HOME="/pnpm"
RUN --mount=type=cache,target=/pnpm/store,id=pnpm pnpm install --frozen-lockfile
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
RUN --mount=type=cache,target=/root/.npm,id=npm <<EOF
npm install
EOF
```

## What this rule removes (and why)

This cleanup only happens when the fix adds cache mounts for the related package manager.

The motivation is simple: these commands/flags either delete local package caches or explicitly disable caching, which cancels out the speed benefits
of cache mounts.

### Cache-cleaning commands removed

- **apt/apt-get**: `apt-get clean`, `apt clean`, and `rm -rf /var/lib/apt/lists*`
- **apk**: `apk cache clean ...` and `rm -rf /var/cache/apk*`
- **dnf**: `dnf clean ...` and `rm -rf /var/cache/dnf*`
- **yum**: `yum clean ...` and `rm -rf /var/cache/yum*`
- **zypper**: `zypper clean ...` and `rm -rf /var/cache/zypp*`
- **npm**: `npm cache clean ...`
- **pnpm**: `pnpm store prune`
- **pip**: `pip cache purge`, `pip cache remove ...`
- **bundle**: `bundle clean ...`
- **yarn**: `yarn cache clean ...`
- **dotnet**: `dotnet nuget locals ... --clear`
- **composer**: `composer clear-cache`, `composer clearcache`
- **uv**: `uv cache clean`, `uv cache prune`
- **bun**: `bun pm cache rm`, `bun pm cache clean`

### Cache-disabling flags removed

- **apk**: `--no-cache`
- **pip**: `--no-cache-dir`
- **uv**: `--no-cache`
- **bun**: `--no-cache`

### Cache-disabling environment variables removed

- **pip**: `ENV PIP_NO_CACHE_DIR=...` (the entire `ENV` instruction is removed if it only sets `PIP_NO_CACHE_DIR`; otherwise, only the
  `PIP_NO_CACHE_DIR` variable is removed)
- **uv**: `ENV UV_NO_CACHE=...` (the entire `ENV` instruction is removed if it only sets `UV_NO_CACHE`; otherwise, only the `UV_NO_CACHE` variable is
  removed)

## References

- [Docker cache optimization: Use cache mounts](https://docs.docker.com/build/cache/optimize/#use-cache-mounts)
- [Dockerfile `RUN --mount` reference](https://docs.docker.com/reference/dockerfile/#run---mount)
- [Using pnpm with Docker](https://pnpm.io/docker)
- [Using uv with Docker: Caching](https://docs.astral.sh/uv/guides/integration/docker/#caching)
