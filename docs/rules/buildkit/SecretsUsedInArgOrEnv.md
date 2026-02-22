# buildkit/SecretsUsedInArgOrEnv

Potentially sensitive data should not be used in the ARG or ENV commands.

| Property  | Value    |
|-----------|----------|
| Severity  | Warning  |
| Category  | Security |
| Default   | Enabled  |

## Description

While it is common to pass secrets to running processes through environment
variables during local development, setting secrets in a Dockerfile using `ENV`
or `ARG` is insecure because they persist in the final image. This rule reports
violations where `ENV` and `ARG` keys indicate that they contain sensitive data.

Instead of `ARG` or `ENV`, you should use secret mounts, which expose secrets
to your builds in a secure manner and do not persist in the final image or its
metadata.

## Examples

Bad:

```dockerfile
FROM scratch
ARG AWS_SECRET_ACCESS_KEY
```

Good (use secret mounts instead):

```dockerfile
FROM scratch
RUN --mount=type=secret,id=aws_key \
    AWS_SECRET_ACCESS_KEY=$(cat /run/secrets/aws_key) \
    aws s3 cp ...
```

See also: [tally/secrets-in-code](../tally/secrets-in-code.md) complements this
rule by detecting actual secret *values* (not just variable names).

## Reference

- [buildkit/SecretsUsedInArgOrEnv](https://docs.docker.com/reference/build-checks/secrets-used-in-arg-or-env/)
- [Build secrets](https://docs.docker.com/build/building/secrets/)
