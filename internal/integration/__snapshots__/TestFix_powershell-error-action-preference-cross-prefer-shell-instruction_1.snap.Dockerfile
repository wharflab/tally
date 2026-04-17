FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true; $ProgressPreference = 'SilentlyContinue';"]
RUN Install-Module PSReadLine -Force
RUN Invoke-WebRequest https://example.com -OutFile /tmp/f.zip
