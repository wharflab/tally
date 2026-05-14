Fixed 9 issues
Skipped 6 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**13 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 12 | ⚠️ PowerShell RUN is missing $ErrorActionPreference = 'Stop' and $PSNativeCommandUseErrorActionPreference = $true |
| 12 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
| 12 | 💅 consecutive RUN instructions can be combined using heredoc syntax |
| 12 | ⚠️ 'iwr' is an alias of 'Invoke-WebRequest'. Alias can introduce possible problems and make scripts hard to maintain. Please consider changing alias to its full content. |
| 12 | ⚠️ 'iex' is an alias of 'Invoke-Expression'. Alias can introduce possible problems and make scripts hard to maintain. Please consider changing alias to its full content. |
| 12 | ⚠️ Invoke-Expression is used. Please remove Invoke-Expression from script and find other options instead. |
| 17 | 💅 multiple consecutive spaces (1 extra) |
| 19 | 💅 unexpected blank line between RUN and RUN |
| 21 | 💅 unexpected blank line between RUN and RUN |
| 29 | 💅 unexpected blank line between RUN and RUN |
| 33 | 💅 expected blank line between COPY and RUN |
