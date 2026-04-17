FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop'; $PSNativeCommandUseErrorActionPreference = $true;", "$ProgressPreference = 'SilentlyContinue';"]
RUN Invoke-WebRequest https://example.com/a.zip -OutFile /tmp/a.zip; Write-Host done
