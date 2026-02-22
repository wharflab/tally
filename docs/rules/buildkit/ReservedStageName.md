# buildkit/ReservedStageName

`scratch` is reserved and should not be used as a stage name.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |

## Description

Reserved words should not be used as names for stages in multi-stage builds.
The reserved words are: `context`, `scratch`.

Using a reserved word as a stage name can conflict with built-in BuildKit
behavior and produce confusing build errors.

## Examples

Bad:

```dockerfile
FROM alpine AS scratch
FROM alpine AS context
```

Good:

```dockerfile
FROM alpine AS builder
```

## Reference

- [buildkit/ReservedStageName](https://docs.docker.com/reference/build-checks/reserved-stage-name/)
