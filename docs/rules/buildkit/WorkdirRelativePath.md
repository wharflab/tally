# buildkit/WorkdirRelativePath

Relative workdir can have unexpected results if the base image changes.

| Property  | Value         |
|-----------|---------------|
| Severity  | Warning       |
| Category  | Best Practice |
| Default   | Enabled       |

## Description

When specifying `WORKDIR` in a build stage, you can use an absolute path, like
`/build`, or a relative path, like `./build`. Using a relative path means that
the working directory is relative to whatever the previous working directory was.

This rule warns if you use `WORKDIR` with a relative path without first
specifying an absolute path in the same Dockerfile. The rationale is that using
a relative working directory for a base image built externally is prone to
breaking, since the working directory may change upstream without warning.

## Examples

Bad (assumes `WORKDIR` in base image is `/`):

```dockerfile
FROM nginx AS web
WORKDIR usr/share/nginx/html
COPY public .
```

Good:

```dockerfile
FROM nginx AS web
WORKDIR /usr/share/nginx/html
COPY public .
```

## Supersedes

- [hadolint/DL3000](../hadolint/DL3000.md)

## Reference

- <https://docs.docker.com/reference/build-checks/workdir-relative-path/>
