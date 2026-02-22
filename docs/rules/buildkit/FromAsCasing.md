# buildkit/FromAsCasing

The `AS` and `FROM` keywords' casing should match.

| Property  | Value     |
|-----------|-----------|
| Severity  | Warning   |
| Category  | Style     |
| Default   | Enabled   |
| Auto-fix  | Yes (`--fix`) |

## Description

While Dockerfile keywords can be either uppercase or lowercase, mixing case
styles is not recommended for readability. This rule reports violations where
mixed case style occurs for a `FROM` instruction with an `AS` keyword declaring
a stage name.

## Examples

Bad:

```dockerfile
FROM debian:latest as builder
```

`FROM` is uppercase but `as` is lowercase.

Good:

```dockerfile
FROM debian:latest AS deb-builder
```

```dockerfile
from debian:latest as deb-builder
```

Both keywords use the same casing.

## Auto-fix

The fix changes the `AS` keyword casing to match the `FROM` keyword.

```dockerfile
# Before
FROM alpine as builder

# After (with --fix)
FROM alpine AS builder
```

## Related

- [buildkit/ConsistentInstructionCasing](../buildkit/ConsistentInstructionCasing.md)

## Reference

- [buildkit/FromAsCasing](https://docs.docker.com/reference/build-checks/from-as-casing/)
