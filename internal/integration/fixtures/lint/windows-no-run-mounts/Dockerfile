# syntax=docker/dockerfile:1

# Linux build stage — mounts are fine here
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
RUN --mount=type=cache,target=/root/.nuget/packages dotnet restore
RUN dotnet publish -c Release -o /app

# Windows runtime stage — mounts will fail
FROM mcr.microsoft.com/windows/servercore:ltsc2022

# cache mount: moby/buildkit#5678
RUN --mount=type=cache,target=C:\Users\ContainerUser\.nuget\packages dotnet restore

# secret mount: moby/buildkit#5273
RUN --mount=type=secret,id=nuget_token cmd /C type C:\run\secrets\nuget_token

# ssh mount: moby/buildkit#4837
RUN --mount=type=ssh git clone git@github.com:org/repo.git

# Clean RUN without mount — should NOT trigger
RUN powershell -Command Write-Host "Hello from Windows"
