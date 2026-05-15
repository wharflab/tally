# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Linux build stage — COPY --chown is valid here
FROM mcr.microsoft.com/dotnet/sdk:8.0@sha256:ebecce75fb42c4c14db108f88054d20a93b0b9e6dfbacad56c8f744c342cf9ef AS build
COPY --chown=app:app . /src/
RUN dotnet publish -c Release -o /app

# Windows runtime stage — --chown is silently ignored
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185

# COPY with --chown — should trigger (violation + fix)
COPY --chown=ContainerUser --from=build /app C:/app/

# ADD with --chown — should trigger (violation + fix)
ADD --chown=1000:1000 config.tar.gz C:/config/

# COPY without --chown — should NOT trigger
COPY README.md C:/app/README.md
