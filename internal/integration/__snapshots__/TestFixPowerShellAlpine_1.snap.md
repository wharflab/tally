Fixed 3 issues
Skipped 1 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**5 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 1 | ❌ Base image mcr.microsoft.com/powershell:6.2.1-alpine-3.8 was pulled with platform "[linux/amd64]", expected "linux/arm64" for current build |
| 16 | ⚠️ both wget and curl are used; pick one to reduce image size and complexity |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 16 | 💅 unexpected blank line between RUN and RUN |

