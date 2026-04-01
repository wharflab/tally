Fixed 9 issues
Skipped 2 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**5 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 16 | ⚠️ both wget and curl are used; pick one to reduce image size and complexity |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 8 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 14 | 💅 packages in apk add are not sorted alphabetically |
| 16 | 💅 unexpected blank line between RUN and RUN |

