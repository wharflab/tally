Fixed 1 issues
Skipped 1 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**4 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 1 | ❌ Base image mcr.microsoft.com/windows/servercore/iis:windowsservercore-ltsc2019 was pulled with platform "[windows/amd64]", expected "linux/arm64" for current build |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 17 | 💅 expected blank line between WORKDIR and COPY |

