# Project Overview

## Purpose

`tally` is a fast, configurable linter for Dockerfiles and Containerfiles. It checks container build files for best practices, security issues, and
common mistakes.

## Tech Stack

- **Language**: Go 1.26.0
- **CLI Framework**: `github.com/urfave/cli/v3`
- **Configuration**: `github.com/knadh/koanf/v2` (supports TOML, env vars)
- **Dockerfile Parsing**: `github.com/moby/buildkit/frontend/dockerfile/parser` (official parser)
- **Testing**: `github.com/gkampitakis/go-snaps` (snapshot testing)
- **Concurrency**: `golang.org/x/sync`

## Design Philosophy

**Minimize code ownership** - Heavily reuses existing, well-maintained libraries. Do NOT re-implement functionality that exists in standard libraries.

**Adding dependencies** - Before adding new dependency, run `go list -m -versions <module>` to check available versions and use the latest stable
release.

## Platform

- Development on Darwin (macOS)
- Cross-platform builds supported (see `.goreleaser.yml`)
- Published to NPM, PyPI, and RubyGems
