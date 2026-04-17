FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force
