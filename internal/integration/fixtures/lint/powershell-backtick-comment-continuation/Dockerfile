# escape=`
FROM mcr.microsoft.com/powershell:7.4-ubuntu-22.04@sha256:62300a213a9293916333df2b014cd3a8f22fb0b0b65f2bb446aaf436bcf8c868

SHELL ["pwsh", "-NoLogo", "-NoProfile", "-Command", "$ErrorActionPreference = 'Stop';"]

RUN Set-Content -Path /tmp/comment-proof.txt -Value 'before' `
    # Dockerfile comment: Docker removes this before invoking PowerShell
    && Add-Content -Path /tmp/comment-proof.txt -Value 'after'
