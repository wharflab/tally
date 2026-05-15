# syntax=docker/dockerfile:1@sha256:2780b5c3bab67f1f76c781860de469442999ed1a0d7992a5efdf2cffc0e3d769

# Linux build stage — mounts are fine here
FROM mcr.microsoft.com/dotnet/sdk:8.0@sha256:ebecce75fb42c4c14db108f88054d20a93b0b9e6dfbacad56c8f744c342cf9ef AS build
RUN --mount=type=cache,target=/root/.nuget/packages dotnet restore
RUN dotnet publish -c Release -o /app

# Windows runtime stage — mounts will fail
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185

# cache mount: moby/buildkit#5678
RUN --mount=type=cache,target=C:\Users\ContainerUser\.nuget\packages dotnet restore

# secret mount: moby/buildkit#5273
RUN --mount=type=secret,id=nuget_token cmd /C type C:\run\secrets\nuget_token

# ssh mount: moby/buildkit#4837
RUN --mount=type=ssh git clone git@github.com:org/repo.git

# Clean RUN without mount — should NOT trigger
RUN powershell -Command Write-Host "Hello from Windows"
