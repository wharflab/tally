FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"]
RUN Install-Module PSReadLine -Force; Write-Host done
