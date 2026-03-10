# tally/prefer-add-unpack

Prefer `ADD --unpack` for downloading and extracting remote archives.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Performance |
| Default | Enabled |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

Flags `RUN` instructions that download a remote tar archive with `curl` / `wget`, Windows `curl.exe` / `wget.exe`, or PowerShell
`Invoke-WebRequest` / `iwr`, and extract it with `tar`, suggesting
[`ADD --unpack <url> <dest>`](https://docs.docker.com/reference/dockerfile/#add---unpack) instead.

`ADD --unpack` is a [BuildKit feature](https://docs.docker.com/build/buildkit/) that downloads and extracts a remote tar archive in a single layer,
reducing image size and build complexity. It is implemented directly in BuildKit's Go codepath, so it works on Windows containers too and avoids
spawning download and extraction processes inside the build container.

## Detected Patterns

1. **Pipe pattern**: `curl -fsSL <url> | tar -xz -C /dest`
2. **Download-then-extract**: `curl -o /tmp/app.tar.gz <url> && tar -xf /tmp/app.tar.gz -C /dest`
3. **wget variants**: Same patterns with `wget` instead of `curl`
4. **Windows cmd variants**: `curl.exe ... -o C:\tmp\app.tar.gz && tar.exe -xf C:\tmp\app.tar.gz -C C:\tools`
5. **PowerShell variants**: `Invoke-WebRequest ... -OutFile C:\tmp\app.tar.gz; tar.exe -xf C:\tmp\app.tar.gz -C C:\tools`

The rule checks that the URL has a recognized archive extension and that a `tar` extraction command is present in the same `RUN` instruction.

## Examples

### Before (violation)

```dockerfile
FROM ubuntu:22.04
RUN curl -fsSL https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | tar -xz -C /usr/local

RUN wget -O /tmp/node.tar.xz https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz && \
    tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1

FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["powershell", "-Command"]
RUN Invoke-WebRequest https://example.com/app.tar.gz -OutFile C:\tmp\app.tar.gz; tar.exe -xf C:\tmp\app.tar.gz -C C:\tools
```

### After (fixed with --fix --fix-unsafe)

```dockerfile
FROM ubuntu:22.04
ADD --unpack https://go.dev/dl/go1.22.0.linux-amd64.tar.gz /usr/local

ADD --unpack https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz /usr/local

FROM mcr.microsoft.com/windows/servercore:ltsc2022
SHELL ["powershell", "-Command"]
ADD --unpack https://example.com/app.tar.gz C:\tools
```

## Auto-fix Conditions

The auto-fix is only emitted when:

- The `RUN` instruction contains **only** download and extraction commands (`curl` / `wget` / `curl.exe` / `wget.exe` / `Invoke-WebRequest` / `iwr` +
  `tar`)
- A `tar` extraction command is present (`ADD --unpack` only handles tar archives)

If additional commands are present (e.g. `chmod`, `rm`, `mv`), the violation is still reported but no fix is suggested, since those commands would be
lost.

The tar destination is extracted from `-C`, `--directory=`, or `--directory` flags. If no destination is specified, the effective `WORKDIR` is used.

## Limitations

- PowerShell and Windows support is limited to download-then-extract patterns; POSIX-style pipe detection remains POSIX-shell-only
- Only detects `tar` extraction (`ADD --unpack` does not handle single-file decompressors)
- Does not match ZIP-oriented flows such as `Expand-Archive`
- URL must have a recognized archive file extension

## Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | true | Enable or disable the rule |

## Configuration

```toml
[rules.tally.prefer-add-unpack]
enabled = true
```

## References

- [Dockerfile `ADD` reference](https://docs.docker.com/reference/dockerfile/#add)
- [`ADD --unpack` flag](https://docs.docker.com/reference/dockerfile/#add---unpack)
- [BuildKit overview](https://docs.docker.com/build/buildkit/)
