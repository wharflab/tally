# buildkit/CopyIgnoredFile

Attempting to Copy file that is excluded by .dockerignore.

| Property  | Value       |
|-----------|-------------|
| Severity  | Warning     |
| Category  | Correctness |
| Default   | Enabled     |

## Description

When you use the `ADD` or `COPY` instructions in a Dockerfile, you should
ensure that the files to be copied into the image do not match a pattern present
in `.dockerignore`. Files which match the patterns in a `.dockerignore` file are
not present in the context of the image when it is built. Trying to copy or add
a file which is missing from the context will result in a build error.

## Examples

Given a `.dockerignore` containing `*/tmp/*`:

Bad:

```dockerfile
FROM scratch
COPY ./tmp/helloworld.txt /helloworld.txt
```

Good:

```dockerfile
FROM scratch
COPY ./forever/helloworld.txt /helloworld.txt
```

## Reference

- [buildkit/CopyIgnoredFile](https://docs.docker.com/reference/build-checks/copy-ignored-file/)
