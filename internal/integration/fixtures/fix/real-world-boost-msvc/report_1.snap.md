Fixed 21 issues
Skipped 1 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**8 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 7 | ⚠️ Invoke-Expression is used. Please remove Invoke-Expression from script and find other options instead. |
| 13 | 💅 expected blank line between COPY and RUN |
| 13 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 24 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 24 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
| 28 | ❌ Script definition uses ConvertTo-SecureString with plaintext. This will expose secure information. Encrypted standard strings should be used instead. |
