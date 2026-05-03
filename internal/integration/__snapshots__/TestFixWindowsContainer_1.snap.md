Fixed 6 issues
Skipped 2 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**6 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 4 | ⚠️ Invoke-Expression is used. Please remove Invoke-Expression from script and find other options instead. |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 4 | 💅 consecutive RUN instructions can be combined using heredoc syntax |
| 17 | 💅 expected blank line between WORKDIR and COPY |
| 22 | 💅 PowerShell Invoke-WebRequest without $ProgressPreference = 'SilentlyContinue' |
