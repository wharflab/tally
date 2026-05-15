# Stage 1: SHELL with full prelude — no violations expected
FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469 AS clean
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"]
RUN Install-Module PSReadLine -Force; Write-Host "done"

# Stage 2: SHELL without prelude — multi-command RUNs should trigger
FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469 AS missing-both
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force; Write-Host "one"
RUN Install-Module Az -Force; Write-Host "two"

# Stage 3: SHELL has Stop but no native preference
FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469 AS missing-native
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop';"]
RUN Install-Module PSReadLine -Force; Write-Host "three"

# Stage 4: Explicit wrapper from non-PS shell
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS explicit-wrapper
RUN pwsh -Command "Install-Module PSReadLine -Force; Write-Host done"

# Stage 5: Single-command RUN should not trigger (default threshold=2)
FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469 AS single-cmd
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force
