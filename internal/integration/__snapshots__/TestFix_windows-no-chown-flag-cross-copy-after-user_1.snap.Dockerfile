FROM mcr.microsoft.com/windows/servercore:ltsc2022
USER ContainerUser
COPY src/ C:/app/
