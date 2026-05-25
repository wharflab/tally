Fixed 11 issues
**11 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| - | ℹ️ `HEALTHCHECK` instruction missing |
| 23 | ❌ Default value for ARG ${powershellImage} results in empty or invalid base image name |
| 40 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 44 | ⚠️ Invoke-Expression is used. Please remove Invoke-Expression from script and find other options instead. |
| 54 | ❌ Default value for ARG ${nanoserverImage} results in empty or invalid base image name |
| 56 | ⚠️ Usage of undefined variable '$ProgramFiles' |
| 67 | 💅 expected blank line between USER and RUN |
| 68 | 💅 expected blank line between RUN and USER |
| 81 | ⚠️ COPY without --chown creates root-owned files despite USER ContainerUser |
| 90 | 💅 expected blank line between USER and RUN |
| 92 | 💅 expected blank line between RUN and USER |
