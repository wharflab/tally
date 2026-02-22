# buildkit/InvalidDefinitionDescription

Comments for build stages or arguments should follow the description format.

| Property  | Value     |
|-----------|-----------|
| Severity  | Info      |
| Category  | Style     |
| Default   | Off (experimental) |
| Auto-fix  | Yes (`--fix`) |

## Description

Comments for build stages or arguments should follow the format:
`# <arg/stage name> <description>`. If a comment is not intended to be a
description, add an empty line or comment between the instruction and the
comment.

The `--call=outline` and `--call=targets` flags for the `docker build` command
print descriptions for build targets and arguments. The descriptions are
generated from Dockerfile comments that immediately precede the `FROM` or `ARG`
instruction and that begin with the name of the build stage or argument.

## Examples

Bad:

```dockerfile
# a non-descriptive comment
FROM scratch AS base

# another non-descriptive comment
ARG VERSION=1
```

Good (empty line separating):

```dockerfile
# a non-descriptive comment

FROM scratch AS base

# another non-descriptive comment

ARG VERSION=1
```

Good (proper description format):

```dockerfile
# base is a stage for compiling source
FROM scratch AS base
# VERSION This is the version number.
ARG VERSION=1
```

## Auto-fix

The fix inserts an empty line between the non-description comment and the
instruction.

```dockerfile
# Before
# Some comment
FROM alpine AS builder

# After (with --fix)
# Some comment

FROM alpine AS builder
```

## Reference

- [buildkit/InvalidDefinitionDescription](https://docs.docker.com/reference/build-checks/invalid-definition-description/)
