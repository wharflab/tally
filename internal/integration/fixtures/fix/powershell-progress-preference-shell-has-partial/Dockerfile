FROM mcr.microsoft.com/powershell:ubuntu-22.04
SHELL ["pwsh", "-Command", "$ErrorActionPreference = 'Stop';"]
RUN Invoke-WebRequest https://example.com/b.zip -OutFile /tmp/b.zip
