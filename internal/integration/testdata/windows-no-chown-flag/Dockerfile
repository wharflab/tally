# syntax=docker/dockerfile:1

# Linux build stage — COPY --chown is valid here
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
COPY --chown=app:app . /src/
RUN dotnet publish -c Release -o /app

# Windows runtime stage — --chown is silently ignored
FROM mcr.microsoft.com/windows/servercore:ltsc2022

# COPY with --chown — should trigger (violation + fix)
COPY --chown=ContainerUser --from=build /app C:/app/

# ADD with --chown — should trigger (violation + fix)
ADD --chown=1000:1000 config.tar.gz C:/config/

# COPY without --chown — should NOT trigger
COPY README.md C:/app/README.md
