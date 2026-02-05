# Rules Reference

tally supports rules from multiple sources, each with its own namespace prefix.

## Rule Namespaces

| Namespace | Source | Description |
|-----------|--------|-------------|
| [`tally/`](./tally/) | tally | Custom rules implemented by tally |
| `buildkit/` | [BuildKit Linter](https://docs.docker.com/reference/build-checks/) | Docker's official Dockerfile checks |
| `hadolint/` | [Hadolint](https://github.com/hadolint/hadolint) | Shell best practices (DL/SC rules) |

## Quick Links

- [tally Rules](./tally/) - Custom rules for security, maintainability, and style
- BuildKit Rules - Docker's official checks (coming soon)
- Hadolint Rules - Shell best practices (coming soon)

## Configuration

Configure rules in `.tally.toml`:

```toml
[rules]
# Enable/disable rules by pattern
include = ["buildkit/*"]                     # Enable all buildkit rules
exclude = ["buildkit/MaintainerDeprecated"]  # Disable specific rules

# Configure rule options
[rules.tally.max-lines]
severity = "warning"
max = 100
skip-blank-lines = true
skip-comments = true
```

## Inline Directives

Suppress rules using inline comments:

```dockerfile
# tally ignore=buildkit/StageNameCasing
FROM alpine AS Build

# hadolint ignore=DL3024
FROM alpine AS builder
```

See [Configuration Guide](../guide/configuration.md) for more details.
