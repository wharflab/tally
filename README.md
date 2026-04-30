# tally

[![codecov](https://codecov.io/gh/wharflab/tally/graph/badge.svg?token=J3vK0hyLkf)](https://codecov.io/gh/wharflab/tally)

tally is a production-grade **Dockerfile/Containerfile linter + formatter** that keeps build files clean, modern, and consistent.

It uses **BuildKit's official parser and checks** (the same foundation behind `docker buildx`) plus a **safe auto-fix** engine. It runs fast,
doesn't require Docker Desktop or a daemon, and fits neatly into CI.

```bash
# Lint everything in the repo (recursive)
tally lint .

# Apply all safe fixes automatically
tally lint --fix Dockerfile
```

## Why tally

Modern Dockerfiles deserve modern tooling. tally is opinionated in the right places:

- **BuildKit-native**: understands modern syntax like heredocs, `RUN --mount=...`, `COPY --link`, and `ADD --checksum=...`.
- **Fixes, not just findings**: `--fix` applies safe, mechanical rewrites; `--fix-unsafe` unlocks opt-in risky fixes (including AI).
- **Modernizes on purpose**: converts eligible `RUN`/`COPY` instructions to heredocs, prefers BuildKit `ADD` sources for archives and git repos, and
  more.
- **Broad rule coverage**: combines Docker's official BuildKit checks, embedded ShellCheck for shell snippets, Hadolint-compatible rules, and
  tally-specific rules.
- **PowerShell-aware**: parses full PowerShell syntax for semantic tokens and rule analysis, so PowerShell `RUN` instructions are treated as real
  code instead of opaque strings.
- **Windows-container aware**: detects Windows container OS, understands Windows paths and default shells, and recognizes `cmd.exe` and
  PowerShell-specific build patterns.
- **Registry-aware without Docker**: uses a Podman-compatible registry client for image metadata checks (no daemon required).
- **Editor + CI friendly**: VS Code extension (`wharflab.tally`, powered by `tally lsp`) and outputs for JSON, SARIF, and GitHub Actions annotations.
- **Easy to install anywhere**: Homebrew, WinGet, Go, npm, Bun, uv, pip, and RubyGems.
- **Written in Go**: single fast binary, built on production-grade libraries.

Quality bar: **92% code coverage on Codecov** and **2,900+ Go tests executed in CI**.

## Documentation

For installation, usage, configuration, rules reference, and more, visit the full documentation at
**[tally.wharflab.com](https://tally.wharflab.com/)**.

## Contributing

See [CLAUDE.md](CLAUDE.md) for development guidelines.

## License

GPL-3.0-only. See [LICENSE](LICENSE) for the full license text.
