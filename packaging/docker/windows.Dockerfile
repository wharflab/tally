# escape=`

FROM mcr.microsoft.com/windows/nanoserver:ltsc2025

LABEL org.opencontainers.image.title="tally" `
      org.opencontainers.image.description="Production-grade Dockerfile and Containerfile linter + formatter." `
      org.opencontainers.image.vendor="Wharflab" `
      org.opencontainers.image.licenses="GPL-3.0-only"

USER ContainerUser

COPY ["tally.exe", "C:\\tally\\tally.exe"]

ENTRYPOINT ["C:\\tally\\tally.exe"]
