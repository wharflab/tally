Fixed 12 issues
**10 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| - | ℹ️ `HEALTHCHECK` instruction missing |
| 3 | ⚠️ set the SHELL option -o pipefail before RUN with a pipe in it |
| 3 | ℹ️ use `ADD --unpack <url> <dest>` instead of downloading and extracting in `RUN` |
| 51 | ❌ file has 200 lines, maximum allowed is 50 |
| 92 | ℹ️ echo may not expand escape sequences. Use printf. |
| 122 | ⚠️ use WORKDIR to switch to a directory |
| 122 | ⚠️ set the SHELL option -o pipefail before RUN with a pipe in it |
| 164 | ⚠️ set the SHELL option -o pipefail before RUN with a pipe in it |
| 164 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 210 | 💅 expected blank line between EXPOSE and CMD |
