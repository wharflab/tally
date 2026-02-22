# buildkit/UndefinedArgInFrom

FROM argument is not declared.

| Property | Value |
|----------|-------|
| Severity | Warning |
| Category | Correctness |
| Default | Enabled |

## Description

This rule warns for cases where you are consuming an undefined build argument
in `FROM` instructions.

Interpolating build arguments in `FROM` instructions can be a good way to add
flexibility to your build. However, if the argument is never declared with
`ARG`, the variable silently resolves to an empty string, which is almost
certainly not the intended behavior.

This check also tries to detect and warn when a `FROM` instruction references
misspelled built-in build arguments, like `BUILDPLATFORM`.

## Examples

Bad:

```dockerfile
FROM node:22${VARIANT} AS jsbuilder
```

Good:

```dockerfile
ARG VARIANT="-alpine3.20"
FROM node:22${VARIANT} AS jsbuilder
```

You can also pass the argument at build time:

```console
docker buildx build --build-arg ALPINE_VERSION=edge .
```

## Reference

- [buildkit/UndefinedArgInFrom](https://docs.docker.com/reference/build-checks/undefined-arg-in-from/)
