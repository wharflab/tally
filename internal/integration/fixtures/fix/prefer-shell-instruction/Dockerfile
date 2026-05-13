FROM mcr.microsoft.com/powershell:ubuntu-22.04

RUN pwsh -NoLogo -NoProfile -Command Install-Module PSReadLine -Force
ENV POWERSHELL_TELEMETRY_OPTOUT=1
RUN pwsh -NoLogo -NoProfile -Command Invoke-WebRequest https://example.com/tools.zip -OutFile /tmp/tools.zip
