# escape=`
FROM mcr.microsoft.com/powershell:7.4-ubuntu-22.04

SHELL ["pwsh", "-NoLogo", "-NoProfile", "-Command", "$ErrorActionPreference = 'Stop';"]

RUN Set-Content -Path /tmp/comment-proof.txt -Value 'before' `
    # Dockerfile comment: Docker removes this before invoking PowerShell
    && Add-Content -Path /tmp/comment-proof.txt -Value 'after'
