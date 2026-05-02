note: 4 slow check(s) skipped (image not found)
Fixed 20 issues
Skipped 2 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**9 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 28 | ❌ Script definition uses ConvertTo-SecureString with plaintext. This will expose secure information. Encrypted standard strings should be used instead. |
| 7 | ⚠️ Invoke-Expression is used. Please remove Invoke-Expression from script and find other options instead. |
| 13 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 24 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 13 | 💅 expected blank line between COPY and RUN |
| 21 | 💅 trailing whitespace |
| 24 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
