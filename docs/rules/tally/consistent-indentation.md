# tally/consistent-indentation

Enforces consistent indentation for Dockerfile build stages.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Off (experimental) |
| Auto-fix | Yes (safe) |

## Description

Enforces consistent indentation to visually separate build stages in multi-stage Dockerfiles. This rule always uses **tabs** for indentation.

**Behavior depends on the number of stages:**

- **Multi-stage** (2+ FROM instructions): Commands within each stage must be indented with 1 tab. FROM lines remain at column 0.
- **Single-stage** (1 FROM instruction): All indentation is removed — tabs, spaces, or any mix. Since there is no stage structure to communicate,
  indenting commands adds noise. The auto-fix strips all leading whitespace from every instruction.

### Why tabs only?

Docker heredoc syntax (`<<-`) strips **leading tabs** from body lines. Spaces have no equivalent shell whitespace treatment — using them for
indentation would corrupt heredoc content when `<<-` is applied. Because this rule must convert `<<` to `<<-` when adding indentation to heredoc
instructions, only tabs produce correct results.

```dockerfile
FROM alpine:3.20
	COPY <<-EOF /etc/config
		key=value
		other=setting
	EOF
```

With spaces, `<<-` cannot strip indentation, so the content would retain unwanted leading whitespace.

### Multi-stage (indentation required)

```dockerfile
FROM golang:1.23 AS builder
	WORKDIR /src
	COPY . .
	RUN go build -o /app

FROM alpine:3.20
	COPY --from=builder /app /usr/local/bin/app
	ENTRYPOINT ["app"]
```

### Single-stage (no indentation)

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache curl
COPY . /app
CMD ["./app"]
```

## Examples

### Bad (multi-stage without indentation)

```dockerfile
FROM golang:1.23 AS builder
WORKDIR /src
RUN go build -o /app
FROM alpine:3.20
COPY --from=builder /app /app
```

### Good (multi-stage with tab indentation)

```dockerfile
FROM golang:1.23 AS builder
	WORKDIR /src
	RUN go build -o /app
FROM alpine:3.20
	COPY --from=builder /app /app
```

### Bad (single-stage with indentation)

In a single-stage Dockerfile, indentation is unnecessary and will be removed by `--fix`:

```dockerfile
# Before (violation: unexpected indentation)
FROM alpine:3.20
	RUN apk add curl
	COPY . /app

# After --fix (indentation removed)
FROM alpine:3.20
RUN apk add curl
COPY . /app
```

## Configuration

Enable the rule (no configurable options — tabs are always used):

```toml
[rules.tally.consistent-indentation]
severity = "style"
```

## Auto-fix

This rule provides safe auto-fixes that adjust indentation:

- **Multi-stage**: Adds 1 tab indentation to commands within stages
- **Single-stage**: Removes all leading whitespace (tabs and spaces) from commands
- **Style correction**: Replaces wrong indent characters (e.g., spaces to tabs)
- **Heredoc `<<-` conversion**: When tab indentation is applied to a heredoc instruction (`RUN <<EOF`, `COPY <<EOF`), the fix converts `<<` to `<<-`
  so that BuildKit strips the leading tabs from the heredoc body

```bash
tally lint --fix Dockerfile
```
