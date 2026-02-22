# buildkit/ConsistentInstructionCasing

Instruction keywords should use consistent casing throughout the Dockerfile.

| Property  | Value     |
|-----------|-----------|
| Severity  | Warning   |
| Category  | Style     |
| Default   | Enabled   |
| Auto-fix  | Yes (`--fix`) |

## Description

Instruction keywords should use consistent casing (all lowercase or all
uppercase). Using a case that mixes uppercase and lowercase, such as PascalCase
or snakeCase, results in poor readability.

## Examples

Bad:

```dockerfile
From alpine
Run echo hello > /greeting.txt
EntRYpOiNT ["cat", "/greeting.txt"]
```

Good (all uppercase):

```dockerfile
FROM alpine
RUN echo hello > /greeting.txt
ENTRYPOINT ["cat", "/greeting.txt"]
```

Good (all lowercase):

```dockerfile
from alpine
run echo hello > /greeting.txt
entrypoint ["cat", "/greeting.txt"]
```

## Auto-fix

The fix changes instruction keywords to match the majority casing in the
Dockerfile.

```dockerfile
# Before (majority is uppercase)
FROM alpine
run echo hello

# After (with --fix)
FROM alpine
RUN echo hello
```

## Reference

- [buildkit/ConsistentInstructionCasing](https://docs.docker.com/reference/build-checks/consistent-instruction-casing/)
