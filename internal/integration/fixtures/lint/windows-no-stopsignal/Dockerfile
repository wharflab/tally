# Linux build stage — STOPSIGNAL is valid here
FROM mcr.microsoft.com/dotnet/sdk:8.0@sha256:ebecce75fb42c4c14db108f88054d20a93b0b9e6dfbacad56c8f744c342cf9ef AS build
STOPSIGNAL SIGTERM
RUN dotnet publish -c Release -o /app

# Windows runtime stage — STOPSIGNAL has no effect
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185
STOPSIGNAL SIGTERM
CMD ["dotnet", "/app/MyApp.dll"]
