# buildkit/DuplicateStageName

Stage names in a multi-stage build should be unique.

| Property  | Value       |
|-----------|-------------|
| Severity  | Error       |
| Category  | Correctness |
| Default   | Enabled     |

## Description

Defining multiple stages with the same name results in an error because the
builder is unable to uniquely resolve the stage name reference.

## Examples

Bad:

```dockerfile
FROM debian:latest AS builder
RUN apt-get update; apt-get install -y curl

FROM golang:latest AS builder
```

Good:

```dockerfile
FROM debian:latest AS deb-builder
RUN apt-get update; apt-get install -y curl

FROM golang:latest AS go-builder
```

## Supersedes

- [hadolint/DL3024](../hadolint/DL3024.md)

## Reference

- [buildkit/DuplicateStageName](https://docs.docker.com/reference/build-checks/duplicate-stage-name/)
