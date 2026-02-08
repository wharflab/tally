# tally/prefer-add-unpack

Prefer `ADD --unpack` for downloading and extracting remote archives.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Performance |
| Default | Enabled |
| Auto-fix | Yes (`--fix --fix-unsafe`) |

## Description

Flags `RUN` instructions that download a remote archive with `curl`/ `wget` and extract it. Tar-based extractions can be replaced with
[`ADD --unpack <url> <dest>`](https://docs.docker.com/reference/dockerfile/#add---unpack); single-file decompressors are reported without a fix.

`ADD --unpack` is a [BuildKit feature](https://docs.docker.com/build/buildkit/) that downloads and extracts a remote tar archive in a single layer,
reducing image size and build complexity.

## Detected Patterns

1. **Pipe pattern**: `curl -fsSL <url> | tar -xz -C /dest`
2. **Download-then-extract**: `curl -o /tmp/app.tar.gz <url> && tar -xf /tmp/app.tar.gz -C /dest`
3. **wget variants**: Same patterns with `wget` instead of `curl`
4. **Single-file decompressors**: `curl -o /tmp/data.gz <url> && gunzip /tmp/data.gz` (detected but not auto-fixed)

The rule checks that the URL has a recognized archive extension and that an extraction command is present in the same `RUN` instruction.

## Examples

### Before (violation)

```dockerfile
FROM ubuntu:22.04
RUN curl -fsSL https://go.dev/dl/go1.22.0.linux-amd64.tar.gz | tar -xz -C /usr/local

RUN wget -O /tmp/node.tar.xz https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz && \
    tar -xJf /tmp/node.tar.xz -C /usr/local --strip-components=1
```

### After (fixed with --fix --fix-unsafe)

```dockerfile
FROM ubuntu:22.04
ADD --unpack https://go.dev/dl/go1.22.0.linux-amd64.tar.gz /usr/local

ADD --unpack https://nodejs.org/dist/v20.11.0/node-v20.11.0-linux-x64.tar.xz /usr/local
```

## Auto-fix Conditions

The auto-fix is only emitted when:

- The `RUN` instruction contains **only** download and extraction commands (`curl`/`wget` + `tar`)
- A `tar` extraction command is present (`ADD --unpack` only handles tar archives)

If additional commands are present (e.g. `chmod`, `rm`, `mv`), the violation is still reported but no fix is suggested, since those commands would be
lost. Single-file decompressors (`gunzip`, `bunzip2`, etc.) are flagged as violations but not auto-fixed because `ADD --unpack` does not decompress
non-tar files.

The tar destination is extracted from `-C`, `--directory=`, or `--directory` flags. If no destination is specified, the effective `WORKDIR` is used.

## Limitations

- Only detects `curl` and `wget` as download commands
- Auto-fix requires `tar` extraction (single-file decompressors are detected but not auto-fixed)
- Skips non-POSIX shells (e.g. PowerShell stages)
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
