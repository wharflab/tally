# tally/curl-missing-location

curl commands should include `--location` to follow HTTP redirects.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

Flags `curl` commands in `RUN` instructions that are missing the `-L`/`--location` flag.
Without this flag, curl will not follow HTTP redirects (301, 302, 307, 308), which can
cause downloads to silently fail when URLs are relocated.

Other Dockerfile download mechanisms follow redirects by default:

- **`ADD <url>`** follows up to 10 redirects (Go `net/http` default behavior)
- **`wget`** follows up to 20 redirects by default

Using `--location` with `curl` ensures consistent redirect-following behavior across
all download methods in a Dockerfile.

## Examples

### Before (violation)

```dockerfile
FROM ubuntu:22.04
RUN curl -fsSo /tmp/file.tar.gz https://example.com/file.tar.gz
RUN curl https://example.com/script.sh | sh
```

### After (fixed with --fix --fix-unsafe)

```dockerfile
FROM ubuntu:22.04
RUN curl --location -fsSo /tmp/file.tar.gz https://example.com/file.tar.gz
RUN curl --location https://example.com/script.sh | sh
```

## Exceptions

The rule does **not** trigger when:

- `-L` or `--location` is already present (including combined flags like `-fsSL`)
- `--location-trusted` is present (implies redirect following)
- All URL arguments point to IP addresses (e.g., `http://127.0.0.1:8080/health`,
  `http://10.0.0.1/api`), since local/internal services typically don't redirect
- The curl command is a non-transfer invocation (`--help`, `--version`, `--manual`)
  where `--location` has no effect

## Limitations

- Only detects `curl` commands directly visible to the shell parser; commands inside
  variables or dynamically constructed strings are not analyzed
- Skips non-POSIX shells (e.g., PowerShell stages)

## References

- [curl `--location` documentation](https://curl.se/docs/manpage.html#-L)
- [Dockerfile `ADD` reference](https://docs.docker.com/reference/dockerfile/#add)
- Design doc: [Shipwright lessons](../../../design-docs/29-shipwright-lessons-build-aware-repair.md) (Lesson 5)
