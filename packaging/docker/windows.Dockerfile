# escape=`

FROM mcr.microsoft.com/windows/nanoserver:ltsc2025

USER ContainerUser

COPY ["tally.exe", "C:\\tally\\tally.exe"]

HEALTHCHECK NONE

ENTRYPOINT ["C:\\tally\\tally.exe"]
