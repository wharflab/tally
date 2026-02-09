# tally

Fast, modern Dockerfile linter with auto-fixes.

tally keeps Dockerfiles and Containerfiles clean, modern, and consistent — using BuildKit's own parser and checks (the same foundation behind
`docker buildx`) plus safe auto-fixes.

## Quick Start

```bash
# Install via Homebrew
brew install tinovyatkin/tap/tally

# Or via npm/pip/gem
npm install -g tally-cli
pip install tally-cli
gem install tally-cli

# Lint everything in the repo
tally check .

# Apply safe fixes automatically
tally check --fix Dockerfile
```

## Why tally?

- **BuildKit-native parsing** — understands modern syntax like heredocs, `RUN --mount=...`, and `ADD --checksum=...`
- **Fixes, not just findings** — applies safe, mechanical fixes automatically (`--fix`)
- **Easy to install anywhere** — Homebrew, npm, pip, RubyGems, or Go
- **Container ecosystem friendly** — supports Dockerfile/Containerfile conventions
- **Growing ruleset** — BuildKit checks, Hadolint-compatible rules, and tally-specific rules

## Supported Rules

| Source | Rules | Description |
|--------|-------|-------------|
| [BuildKit](https://docs.docker.com/reference/build-checks/) | 12/22 | Docker's official Dockerfile checks |
| tally | 8 | Custom rules including secret detection |
| [Hadolint](https://github.com/hadolint/hadolint) | 27 | Hadolint-compatible rules |

[View all rules →](rules/index.md)

## Next Steps

- [Configuration Guide](guide/configuration.md) — config files, environment variables, CLI flags
- [Rules Reference](rules/index.md) — available rules and how to configure them
