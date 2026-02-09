# tally/prefer-vex-attestation

Prefer attaching OpenVEX VEX documents as OCI attestations instead of copying `*.vex.json` into the image filesystem.

| Property | Value |
|----------|-------|
| Severity | Info |
| Category | Security |
| Default | Enabled |
| Auto-fix | No |

## Description

VEX (Vulnerability Exploitability eXchange) documents are supply-chain metadata describing whether a vulnerability applies to an artifact and why.
Embedding VEX inside the runtime image (for example, `COPY *.vex.json ...`) has drawbacks:

- Requires rebuilding the image to update statements
- Makes discovery less consistent across registries and tooling
- Expands the runtime payload surface

Instead, prefer attaching the OpenVEX document as an **OCI attestation** (in-toto predicate) to the image digest in your CI/CD pipeline.

## Docker Scout discovery behavior

If you do embed VEX documents into the image filesystem, Docker Scout discovers them using these rules:

- **Filename-based:** the VEX document filename must match the `*.vex.json` glob pattern (location in the filesystem doesn't matter).
- **Final image only:** the file must be present in the filesystem of the final image (multi-stage builds must preserve it into the final stage).
- **Attestation precedence:** filesystem-embedded VEX is **ignored** for images that have **any** attestations. If the image has attestations, Scout
  only looks for exceptions in attestations (not in the image filesystem).

See: <https://docs.docker.com/scout/how-tos/create-exceptions-vex/>

## Detected Patterns

- `COPY *.vex.json <dest>`
- `COPY <name>.vex.json <dest>`

## Examples

### Violation

```dockerfile
FROM alpine:3.20
COPY *.vex.json /usr/share/vex/
```

### Recommended direction (conceptual)

Attach VEX as an OCI attestation post-build (tooling varies):

- Docker Scout: `docker scout attestation add --file app.vex.json --predicate-type https://openvex.dev/ns/v0.2.0 <image>`
- cosign: `cosign attest --predicate app.vex.json --type https://openvex.dev/ns/v0.2.0 <image>`

## References

- OpenVEX specification: <https://github.com/openvex/spec>
- Docker Scout VEX exceptions: <https://docs.docker.com/scout/how-tos/create-exceptions-vex/>
- Docker Scout CLI docs: <https://docs.docker.com/scout/>
