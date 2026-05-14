Fixed 16 issues
Skipped 1 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**21 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 26 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 40 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 40 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
| 40 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 49 | 💅 expected 1 blank line between ENV and RUN, found 2 |
| 49 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 49 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
| 49 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 58 | 💅 expected 1 blank line between ENV and RUN, found 2 |
| 58 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
| 65 | 💅 expected 1 blank line between ENV and RUN, found 2 |
| 77 | 💅 expected 1 blank line between RUN and COPY, found 2 |
| 78 | 💅 expected blank line between COPY and RUN |
| 78 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 78 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
| 78 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 90 | 💅 expected blank line between RUN and ENV |
| 91 | ❌ The string is missing the terminator: ". |
| 101 | 💅 expected blank line between ARG and RUN |
| 114 | 💅 expected blank line between COPY and RUN |
