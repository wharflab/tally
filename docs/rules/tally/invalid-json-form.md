# tally/invalid-json-form

Arguments appear to use JSON exec-form but contain invalid JSON.

| Property | Value |
|----------|-------|
| Severity | Error |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | Yes (suggestion, requires `--fix-unsafe`) |

## Description

Several Dockerfile instructions (`CMD`, `ENTRYPOINT`, `RUN`, `SHELL`, `COPY`, `ADD`,
`VOLUME`, `HEALTHCHECK CMD`, `ONBUILD <instruction>`) accept JSON exec-form syntax:

```dockerfile
CMD ["executable", "param1", "param2"]
```

When the arguments start with `[` but contain invalid JSON (unquoted strings, single
quotes, trailing commas), BuildKit's parser silently treats them as shell form. This
produces unexpected behavior:

- `CMD [bash, -lc, "echo hi"]` is treated as the shell command
  `[bash, -lc, "echo hi"]` rather than exec-form `["bash", "-lc", "echo hi"]`.
- `SHELL [/bin/bash, -c]` causes a build error because `SHELL` requires valid JSON.

The auto-fix rewrites the arguments as valid JSON. It is classified as `suggestion`
because intent cannot be guaranteed -- review it before applying.

## Related Rules

- [`buildkit/JSONArgsRecommended`](../buildkit/JSONArgsRecommended.md) -- recommends JSON
  exec-form for `CMD` and `ENTRYPOINT`. Because BuildKit falls back to shell-form when
  JSON is invalid, `JSONArgsRecommended` (info severity) also fires on the same instruction.
  tally's supersession processor automatically suppresses the lower-severity
  `JSONArgsRecommended` violation when this rule (error severity) is present at the same
  line — so users see only the more actionable `invalid-json-form` error.

## References

- [Dockerfile reference -- CMD](https://docs.docker.com/reference/dockerfile/#cmd)
- [Dockerfile reference -- ENTRYPOINT](https://docs.docker.com/reference/dockerfile/#entrypoint)
- [Dockerfile reference -- SHELL](https://docs.docker.com/reference/dockerfile/#shell)

## Examples

### Bad

```dockerfile
FROM alpine:3.20

# Unquoted strings -- treated as shell form
CMD [bash, -lc, "echo hello"]

# Single quotes -- not valid JSON
ENTRYPOINT ['/usr/bin/app', '--serve']

# Trailing comma -- invalid JSON
RUN ["echo", "hello",]
```

### Good

```dockerfile
FROM alpine:3.20

CMD ["bash", "-lc", "echo hello"]
ENTRYPOINT ["/usr/bin/app", "--serve"]
RUN ["echo", "hello"]
```

## Configuration

```toml
[rules.tally.invalid-json-form]
severity = "error"  # Options: "off", "error", "warning", "info", "style"
```
