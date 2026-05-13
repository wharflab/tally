FROM mcr.microsoft.com/powershell:ubuntu-22.04
RUN pwsh -Command Install-Module PSReadLine -Force
RUN pwsh -Command Invoke-WebRequest https://example.com -OutFile /tmp/f.zip
