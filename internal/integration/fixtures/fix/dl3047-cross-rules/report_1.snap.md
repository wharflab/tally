Fixed 6 issues
Skipped 2 fixes
note: 1 AI fix(es) failed (see details below)
note: skipped fix tally/prefer-multi-stage-build (<stdin>): resolver not registered: ai-autofix
**4 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| - | ℹ️ This Dockerfile appears to build artifacts in a single stage; consider a multi-stage build to reduce final image size. |
| 2 | ℹ️ wget without progress bar will bloat build logs; use `wget --progress=dot:giga`, `-q`, or `-nv` |
| 2 | 💅 consecutive RUN instructions can be combined using heredoc syntax |
| 4 | ⚠️ set the SHELL option -o pipefail before RUN with a pipe in it |
