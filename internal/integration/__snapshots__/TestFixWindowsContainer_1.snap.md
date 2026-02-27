Fixed 1 issues
**7 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 1 | ❌ Base image mcr.microsoft.com/windows/servercore/iis:windowsservercore-ltsc2019 was pulled with platform "[windows/amd64]", expected "linux/arm64" for current build |
| 16 | ⚠️ Relative workdir c:/build can have unexpected results if the base image has a WORKDIR set |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| 15 | 💅 unexpected blank line between RUN and RUN |
| 16 | 💅 expected blank line between RUN and WORKDIR |
| 17 | 💅 expected blank line between WORKDIR and COPY |
| 22 | 💅 unexpected blank line between RUN and RUN |

