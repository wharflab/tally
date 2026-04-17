# Stage 1: SHELL prelude already sets $ProgressPreference — no violation expected
FROM mcr.microsoft.com/powershell:ubuntu-22.04 AS clean
SHELL ["pwsh", "-Command", "$ProgressPreference = 'SilentlyContinue';"]
RUN Invoke-WebRequest https://example.com/a.zip -OutFile /tmp/a.zip

# Stage 2: SHELL without prelude — each IWR RUN should trigger
FROM mcr.microsoft.com/powershell:ubuntu-22.04 AS missing-shell
SHELL ["pwsh", "-Command"]
RUN Invoke-WebRequest https://example.com/b.zip -OutFile /tmp/b.zip
RUN iwr https://example.com/c.zip -OutFile /tmp/c.zip

# Stage 3: SHELL has Stop but no ProgressPreference
FROM mcr.microsoft.com/powershell:ubuntu-22.04 AS partial-shell
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop';"]
RUN Invoke-WebRequest https://example.com/d.zip -OutFile /tmp/d.zip

# Stage 4: Explicit wrapper from a non-PowerShell shell (Linux bash)
FROM ubuntu:22.04 AS explicit-wrapper
RUN pwsh -Command "Invoke-WebRequest https://example.com/e.zip -OutFile /tmp/e.zip"

# Stage 5: RUN without Invoke-WebRequest — no violation
FROM mcr.microsoft.com/powershell:ubuntu-22.04 AS no-iwr
SHELL ["pwsh", "-Command"]
RUN Install-Module PSReadLine -Force

# Stage 6: Windows servercore with powershell wrapper and backtick continuation
# escape=`
FROM mcr.microsoft.com/windows/servercore:ltsc2022 AS windows-backtick
RUN powershell -Command "Invoke-WebRequest https://example.com/setup.exe -OutFile C:\\setup.exe"
