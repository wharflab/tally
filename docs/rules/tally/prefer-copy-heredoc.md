# tally/prefer-copy-heredoc

Suggests using COPY heredoc for file creation instead of RUN echo/cat.

| Property | Value |
|----------|-------|
| Severity | Style |
| Category | Style |
| Default | Off (experimental) |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

Suggests replacing `RUN echo/cat/printf > file` patterns with `COPY <<EOF` syntax for better performance and readability.

This rule detects file creation patterns in RUN instructions and extracts them into COPY heredocs, even when mixed with other commands.
It relies on Dockerfile [here-documents](https://docs.docker.com/reference/dockerfile/#here-documents) support for `COPY`.

## Why COPY heredoc?

- **Performance**: `COPY` doesn't spawn a shell container, making it faster
- **Atomicity**: `COPY --chmod` sets permissions in a single layer
- **Readability**: Heredocs are cleaner than escaped echo statements

## Detected Patterns

1. **Simple file creation**: `echo "content" > /path/to/file`
2. **File creation with chmod**: `echo "x" > /file && chmod 0755 /file`
3. **Consecutive RUN instructions** writing to the same file
4. **Mixed commands** with file creation in the middle (extracts just the file creation)

## Examples

### Before (violation)

```dockerfile
RUN cat > /etc/nginx/nginx.conf <<'EOF'
worker_processes auto;
events { worker_connections 1024; }
http {
    server {
        listen 8080;
        location /healthz { return 200 "ok"; }
    }
}
EOF

RUN printf '#!/bin/sh\nexec nginx -g "daemon off;"\n' > /usr/local/bin/start-nginx && \
    chmod 0755 /usr/local/bin/start-nginx

RUN apt-get update && \
    echo "APP_ENV=production" > /etc/myapp.env && \
    echo "LOG_FORMAT=json" >> /etc/myapp.env && \
    apt-get clean
```

### After (fixed with --fix --fix-unsafe)

```dockerfile
COPY <<EOF /etc/nginx/nginx.conf
worker_processes auto;
events { worker_connections 1024; }
http {
    server {
        listen 8080;
        location /healthz { return 200 "ok"; }
    }
}
EOF

COPY --chmod=0755 <<EOF /usr/local/bin/start-nginx
#!/bin/sh
exec nginx -g "daemon off;"
EOF

RUN apt-get update
COPY <<EOF /etc/myapp.env
APP_ENV=production
LOG_FORMAT=json
EOF
RUN apt-get clean
```

## Limitations

- Skips append operations (`>>`) since COPY would change semantics
- Skips relative paths (only absolute paths like `/etc/file`)
- Skips commands with shell variables not defined as ARG/ENV

## Mount Handling

Since `COPY` doesn't support `--mount` flags, the rule handles RUN mounts carefully:

| Mount Type | Behavior |
|------------|----------|
| `bind` | Skip - content might depend on bound files |
| `cache` | Safe if file target is outside cache path |
| `tmpfs` | Safe if file target is outside tmpfs path |
| `secret` | Safe if file target is outside secret path |
| `ssh` | Safe - no content dependency |

When extracting file creation from mixed commands, mounts are preserved on the remaining RUN instructions.

## Chmod Support

Converts both octal and symbolic chmod modes to `COPY --chmod`:

- Octal: `chmod 755` → `--chmod=0755`
- Symbolic: `chmod +x` → `--chmod=0755`, `chmod u+x` → `--chmod=0744`

Symbolic modes are converted based on a 0644 base (default for newly created files).

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `check-single-run` | boolean | true | Check for single RUN instructions with file creation |
| `check-consecutive-runs` | boolean | true | Check for consecutive RUN instructions to same file |

## Configuration

```toml
[rules.tally.prefer-copy-heredoc]
severity = "style"
check-single-run = true
check-consecutive-runs = true
```

## Rule Coordination

This rule takes priority over `prefer-run-heredoc` for pure file creation patterns. When both rules detect a pattern, `prefer-copy-heredoc` handles
it.

## References

- [Dockerfile here-documents](https://docs.docker.com/reference/dockerfile/#here-documents)
