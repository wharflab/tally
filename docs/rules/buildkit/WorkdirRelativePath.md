# buildkit/WorkdirRelativePath

Relative workdir can have unexpected results if the base image changes.

| Property  | Value         |
|-----------|---------------|
| Severity  | Warning       |
| Category  | Best Practice |
| Default   | Enabled       |
| Auto-fix  | Yes (suggestion `--fix-unsafe`; safe with `--slow-checks`) |

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

## Auto-fix

Replaces the relative `WORKDIR` with an absolute path.

- **Fast path** (no registry access): resolves against `/` as a default (`FixSuggestion`, requires `--fix-unsafe`).
- **With `--slow-checks`**: resolves against the base image's actual `WORKDIR` from the registry (`FixSafe`, applied with `--fix`). Chained relative
  WORKDIRs are resolved cumulatively.

```dockerfile
# Before
FROM nginx AS web
WORKDIR usr/share/nginx/html

# After (with --fix-unsafe, fast path — assumes base WORKDIR is /)
FROM nginx AS web
WORKDIR /usr/share/nginx/html

# After (with --fix --slow-checks, base image has WORKDIR /etc/nginx)
FROM nginx AS web
WORKDIR /etc/nginx/usr/share/nginx/html
```

## Supersedes

- [hadolint/DL3000](../hadolint/DL3000.md)

## Reference

- [buildkit/WorkdirRelativePath](https://docs.docker.com/reference/build-checks/workdir-relative-path/)
