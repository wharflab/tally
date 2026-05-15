FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469

RUN pwsh -NoLogo -NoProfile -Command Install-Module PSReadLine -Force
ENV POWERSHELL_TELEMETRY_OPTOUT=1
RUN pwsh -NoLogo -NoProfile -Command Invoke-WebRequest https://example.com/tools.zip -OutFile /tmp/tools.zip
