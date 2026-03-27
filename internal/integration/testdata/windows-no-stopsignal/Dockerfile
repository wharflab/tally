# Linux build stage — STOPSIGNAL is valid here
FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
STOPSIGNAL SIGTERM
RUN dotnet publish -c Release -o /app

# Windows runtime stage — STOPSIGNAL has no effect
FROM mcr.microsoft.com/windows/servercore:ltsc2022
STOPSIGNAL SIGTERM
CMD ["dotnet", "/app/MyApp.dll"]
