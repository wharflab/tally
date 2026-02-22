# buildkit/ExposeProtoCasing

Protocol names in `EXPOSE` instructions should be lowercase.

| Property  | Value     |
|-----------|-----------|
| Severity  | Warning   |
| Category  | Style     |
| Default   | Enabled   |
| Auto-fix  | Yes (`--fix`) |

## Description

Protocol names in the `EXPOSE` instruction should be specified in lowercase to
maintain consistency and readability.

## Examples

Bad:

```dockerfile
EXPOSE 80/TcP
```

Good:

```dockerfile
EXPOSE 80/tcp
```

## Auto-fix

The fix lowercases the protocol in `EXPOSE` port specs.

```dockerfile
# Before
EXPOSE 8080/TCP

# After (with --fix)
EXPOSE 8080/tcp
```

## Reference

- [buildkit/ExposeProtoCasing](https://docs.docker.com/reference/build-checks/expose-proto-casing/)
