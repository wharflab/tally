note: 4 slow check(s) skipped (image not found)
Fixed 15 issues
Skipped 1 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**6 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 13 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 24 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 13 | 💅 expected blank line between COPY and RUN |
| 24 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |

