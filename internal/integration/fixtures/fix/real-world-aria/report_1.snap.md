Fixed 17 issues
Skipped 4 fixes
**21 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 51 | ❌ file has 257 lines, maximum allowed is 50 |
| 81 | ❌ Parsing stopped here. Mismatched keywords or invalid parentheses? |
| 81 | ❌ Unexpected start of line. If breaking lines, \|/\|\|/&& should be at the end of the previous one. |
| 27 | ⚠️ Quote this to prevent word splitting. |
| 39 | ⚠️ Do not use ARG or ENV instructions for sensitive data (HF_TOKEN) |
| 40 | ⚠️ Do not use ARG or ENV instructions for sensitive data (ARIA2_SECRET) |
| 52 | ⚠️ Do not use ARG or ENV instructions for sensitive data (HF_TOKEN) |
| 53 | ⚠️ Do not use ARG or ENV instructions for sensitive data (ARIA2_SECRET) |
| 62 | ⚠️ set the SHELL option -o pipefail before RUN with a pipe in it |
| 283 | ⚠️ Quote this to prevent word splitting. |
| 284 | ⚠️ Quote this to prevent word splitting. |
| 62 | ℹ️ use cache mounts for package manager cache directories |
| 2 | 💅 packages in apt-get install are not sorted alphabetically |
| 8 | 💅 unexpected blank line between RUN and RUN |
| 9 | 💅 expected blank line between RUN and WORKDIR |
| 10 | 💅 expected blank line between WORKDIR and RUN |
| 62 | 💅 split chained commands onto separate lines |
| 62 | 💅 packages in apt-get install are not sorted alphabetically |
| 91 | 💅 expected blank line between ARG and ADD |
| 93 | 💅 expected blank line between ADD and RUN |
| 93 | 💅 consecutive RUN instructions can be combined using heredoc syntax |
