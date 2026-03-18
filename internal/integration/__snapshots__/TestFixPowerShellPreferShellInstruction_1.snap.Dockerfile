FROM mcr.microsoft.com/powershell:ubuntu-22.04

SHELL ["pwsh","-NoLogo","-NoProfile","-Command","$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue';"]
RUN Install-Module PSReadLine -Force
ENV POWERSHELL_TELEMETRY_OPTOUT=1
RUN Invoke-WebRequest https://example.com/tools.zip -OutFile /tmp/tools.zip
