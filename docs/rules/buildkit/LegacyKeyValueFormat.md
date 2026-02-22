# buildkit/LegacyKeyValueFormat

`ENV key=value` should be used instead of the legacy `ENV key value` format.

| Property  | Value     |
|-----------|-----------|
| Severity  | Warning   |
| Category  | Style     |
| Default   | Enabled   |
| Auto-fix  | Yes (`--fix`) |

## Description

The correct format for declaring environment variables and build arguments in a
Dockerfile is `ENV key=value` and `ARG key=value`, where the variable name and
value are separated by an equals sign. Historically, Dockerfiles have also
supported a space separator. This legacy format is deprecated.

## Examples

Bad:

```dockerfile
ARG foo bar
```

Good:

```dockerfile
ARG foo=bar
```

Bad:

```dockerfile
ENV DEPS \
    curl \
    git \
    make
```

Good:

```dockerfile
ENV DEPS="\
    curl \
    git \
    make"
```

## Auto-fix

The fix replaces the whitespace-separated format with equals format. Quotes are
added if the value contains spaces.

```dockerfile
# Before
ENV key value
ENV multi word value

# After (with --fix)
ENV key=value
ENV multi="word value"
```

## Reference

- [buildkit/LegacyKeyValueFormat](https://docs.docker.com/reference/build-checks/legacy-key-value-format/)
