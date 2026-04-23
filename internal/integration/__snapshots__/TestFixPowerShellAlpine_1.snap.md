Fixed 10 issues
Skipped 1 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**4 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 16 | ⚠️ both wget and curl are used; standardize on wget |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 8 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 16 | 💅 unexpected blank line between RUN and RUN |

