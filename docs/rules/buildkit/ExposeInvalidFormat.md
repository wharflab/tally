# buildkit/ExposeInvalidFormat

EXPOSE instruction should not define an IP address or host-port mapping.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |

## Description

The `EXPOSE` instruction in a Dockerfile is used to indicate which ports the
container listens on at runtime. It should not include an IP address or
host-port mapping.

Including an IP address or host-port mapping in the `EXPOSE` instruction does
not actually publish the port and can be misleading. Use `docker run -p` or
`docker compose` port mappings to bind host ports at runtime.

## Examples

Bad:

```dockerfile
FROM alpine
EXPOSE 127.0.0.1:80:80
```

Good:

```dockerfile
FROM alpine
EXPOSE 80
```

Bad:

```dockerfile
FROM alpine
EXPOSE 80:80
```

Good:

```dockerfile
FROM alpine
EXPOSE 80
```

## Reference

- <https://docs.docker.com/reference/build-checks/expose-invalid-format/>
