# Stage 1: SHELL prelude already sets $ProgressPreference — no violation expected
FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469 AS clean
SHELL ["pwsh", "-Command", "$ProgressPreference = 'SilentlyContinue';"]
RUN Invoke-WebRequest https://example.com/a.zip -OutFile /tmp/a.zip

# Stage 2: SHELL without prelude — each IWR RUN should trigger
FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469 AS missing-shell
SHELL ["pwsh", "-Command"]
RUN Invoke-WebRequest https://example.com/b.zip -OutFile /tmp/b.zip
RUN iwr https://example.com/c.zip -OutFile /tmp/c.zip

# Stage 3: SHELL has Stop but no ProgressPreference
FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469 AS partial-shell
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop';"]
RUN Invoke-WebRequest https://example.com/d.zip -OutFile /tmp/d.zip

# Stage 4: Explicit wrapper from a non-PowerShell shell (Linux bash)
FROM ubuntu:22.04@sha256:962f6cadeae0ea6284001009daa4cc9a8c37e75d1f5191cf0eb83fe565b63dd7 AS explicit-wrapper
RUN pwsh -Command "Invoke-WebRequest https://example.com/e.zip -OutFile /tmp/e.zip"

# Stage 5: RUN without Invoke-WebRequest — no violation
FROM mcr.microsoft.com/powershell:ubuntu-22.04@sha256:a3affe99603400235501b8da8be5f9e40152d4db6557f698a91da0280f9e1469 AS no-iwr
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force

# Stage 6: Windows servercore with powershell wrapper and backtick continuation
# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022@sha256:86da395cfd2b35dbfc2e9d08719550c51b0570c394bff8f92622a19234766185 AS windows-backtick
RUN powershell -Command "Invoke-WebRequest https://example.com/setup.exe -OutFile C:\\setup.exe"
