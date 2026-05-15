FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"]
RUN Install-Module PSReadLine -Force; Write-Host done
