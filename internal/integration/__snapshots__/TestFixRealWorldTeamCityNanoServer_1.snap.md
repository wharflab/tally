Fixed 9 issues
Skipped 2 fixes
**14 issues** in `<stdin>`

| Line | Issue |
|------|-------|
| 23 | ❌ Default value for ARG ${powershellImage} results in empty or invalid base image name |
| 54 | ❌ Default value for ARG ${nanoserverImage} results in empty or invalid base image name |
| 33 | ⚠️ COPY without --chown creates root-owned files despite USER ContainerAdministrator |
| 34 | ⚠️ COPY without --chown creates root-owned files despite USER ContainerAdministrator |
| 44 | ⚠️ Invoke-Expression is used. Please remove Invoke-Expression from script and find other options instead. |
| 56 | ⚠️ Usage of undefined variable '$ProgramFiles' |
| 76 | ⚠️ Script definition uses Write-Host. Avoid using Write-Host because it might not work in all hosts, does not work when there is no host, and (prior to PS 5.0) cannot be suppressed, captured, or redirected. Instead, use Write-Output, Write-Verbose, or Write-Information. |
| 81 | ⚠️ COPY without --chown creates root-owned files despite USER ContainerUser |
| - | ℹ️ `HEALTHCHECK` instruction missing |
| 40 | 💅 RUN instruction with chained commands can use heredoc syntax |
| 67 | 💅 expected blank line between USER and RUN |
| 68 | 💅 expected blank line between RUN and USER |
| 90 | 💅 expected blank line between USER and RUN |
| 92 | 💅 expected blank line between RUN and USER |
