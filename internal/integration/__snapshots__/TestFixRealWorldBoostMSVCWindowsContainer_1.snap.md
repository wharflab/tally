note: 4 slow check(s) skipped (image not found)
Fixed 16 issues
Skipped 1 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**13 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 28 | ❌ Script definition uses ConvertTo-SecureString with plaintext. This will expose secure information. Encrypted standard strings should be used instead. |
| 7 | ⚠️ 'iex' is an alias of 'Invoke-Expression'. Alias can introduce possible problems and make scripts hard to maintain. Please consider changing alias to its full content. |
| 7 | ⚠️ Invoke-Expression is used. Please remove Invoke-Expression from script and find other options instead. |
| 13 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 20 | ⚠️ 'del' is an alias of 'Remove-Item'. Alias can introduce possible problems and make scripts hard to maintain. Please consider changing alias to its full content. |
| 21 | ⚠️ 'move' is an alias of 'Move-Item'. Alias can introduce possible problems and make scripts hard to maintain. Please consider changing alias to its full content. |
| 24 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 25 | ⚠️ 'cd' is an alias of 'Set-Location'. Alias can introduce possible problems and make scripts hard to maintain. Please consider changing alias to its full content. |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 21 | ℹ️ Line has trailing whitespace |
| 13 | 💅 expected blank line between COPY and RUN |
| 24 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
