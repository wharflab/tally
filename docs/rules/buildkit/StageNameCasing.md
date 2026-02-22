# buildkit/StageNameCasing

Stage names in multi-stage builds should be lowercase.

| Property  | Value     |
|-----------|-----------|
| Severity  | Warning   |
| Category  | Style     |
| Default   | Enabled   |
| Auto-fix  | Yes (`--fix`) |

## Description

Stage name should be lowercase. To help distinguish Dockerfile instruction
keywords from identifiers, this rule forces names of stages in a multi-stage
Dockerfile to be all lowercase.

## Examples

Bad:

```dockerfile
FROM alpine AS BuilderBase
```

Good:

```dockerfile
FROM alpine AS builder-base
```

## Auto-fix

The fix renames the stage to lowercase and updates all references (`FROM`,
`COPY --from`).

```dockerfile
# Before
FROM alpine AS Builder
COPY --from=Builder /app .

# After (with --fix)
FROM alpine AS builder
COPY --from=builder /app .
```

## Reference

- [buildkit/StageNameCasing](https://docs.docker.com/reference/build-checks/stage-name-casing/)
