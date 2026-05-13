# Stage 1: SHELL with full prelude — no violations expected
FROM mcr.microsoft.com/powershell:ubuntu-22.04 AS clean
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;"]
RUN Install-Module PSReadLine -Force; Write-Host "done"

# Stage 2: SHELL without prelude — multi-command RUNs should trigger
FROM mcr.microsoft.com/powershell:ubuntu-22.04 AS missing-both
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force; Write-Host "one"
RUN Install-Module Az -Force; Write-Host "two"

# Stage 3: SHELL has Stop but no native preference
FROM mcr.microsoft.com/powershell:ubuntu-22.04 AS missing-native
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop';"]
RUN Install-Module PSReadLine -Force; Write-Host "three"

# Stage 4: Explicit wrapper from non-PS shell
FROM ubuntu:22.04 AS explicit-wrapper
RUN pwsh -Command "Install-Module PSReadLine -Force; Write-Host done"

# Stage 5: Single-command RUN should not trigger (default threshold=2)
FROM mcr.microsoft.com/powershell:ubuntu-22.04 AS single-cmd
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force
