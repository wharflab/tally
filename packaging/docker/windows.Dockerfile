# escape=`

FROM mcr.microsoft.com/windows/nanoserver:ltsc2025

ARG VERSION=dev
ARG VCS_REF=unknown
ARG SOURCE_URL=https://github.com/wharflab/tally

LABEL org.opencontainers.image.title="tally" `
      org.opencontainers.image.description="Production-grade Dockerfile and Containerfile linter + formatter." `
      org.opencontainers.image.source="${SOURCE_URL}" `
      org.opencontainers.image.url="${SOURCE_URL}" `
      org.opencontainers.image.documentation="${SOURCE_URL}" `
      org.opencontainers.image.version="${VERSION}" `
      org.opencontainers.image.revision="${VCS_REF}" `
      org.opencontainers.image.vendor="Wharflab" `
      org.opencontainers.image.licenses="GPL-3.0-only"

USER ContainerUser

COPY ["tally.exe", "C:\\tally\\tally.exe"]

ENTRYPOINT ["C:\\tally\\tally.exe"]
