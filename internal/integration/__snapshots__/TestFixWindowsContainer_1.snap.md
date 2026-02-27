Fixed 4 issues
**14 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 1 | ❌ Base image mcr.microsoft.com/windows/servercore/iis:windowsservercore-ltsc2019 was pulled with platform "[windows/amd64]", expected "linux/arm64" for current build |
| 4 | ⚠️ env is referenced but not assigned (for output from commands, use "$(env ...)" ). |
| 16 | ⚠️ Relative workdir c:/build can have unexpected results if the base image has a WORKDIR set |
| 24 | ⚠️ \t is just literal 't' here. For tab, use "$(printf '\t')" instead. |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| 15 | ℹ️ This \b will be a regular 'b' in this context. |
| 19 | ℹ️ This \i will be a regular 'i' in this context. |
| 24 | ℹ️ This \W will be a regular 'W' in this context. |
| 25 | ℹ️ This \b will be a regular 'b' in this context. |
| 7 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 15 | 💅 unexpected blank line between RUN and RUN |
| 16 | 💅 expected blank line between RUN and WORKDIR |
| 17 | 💅 expected blank line between WORKDIR and COPY |
| 22 | 💅 unexpected blank line between RUN and RUN |

