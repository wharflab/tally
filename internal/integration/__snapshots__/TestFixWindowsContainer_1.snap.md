Fixed 4 issues
Skipped 1 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**4 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| - | ℹ️ `HEALTHCHECK` instruction missing |
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 4 | 💅 consecutive RUN instructions can be combined using heredoc syntax |
| 17 | 💅 expected blank line between WORKDIR and COPY |

